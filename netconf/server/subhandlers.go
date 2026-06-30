package server

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"orange/sonic-netconf-server/build/netconf_codegen"
	"orange/sonic-netconf-server/lib"

	"github.com/Azure/sonic-mgmt-common/translib"
	"github.com/antchfx/xmlquery"
	"github.com/clbanning/mxj/v2"
	"github.com/gliderlabs/ssh"
	"github.com/go-redis/redis/v7"
	"github.com/golang/glog"
)

var (
	YangSchemas map[string][]Schema
	YangModules ModulesState
	read        = false
	redisClient	*redis.Client
)

func init() {
	redisClient = redis.NewClient(&redis.Options{
		Network:  "unix",
		Addr:     "/var/run/redis/redis.sock",
		Password: "",
		DB:       4,
	})
}

type DbTarget string

const (
	ALL DbTarget = "all"
	CONFIG DbTarget = "config"
)

func readYangModules() {
	// return all schemas in module form
	schemas := []Schema{}
	YangSchemas = make(map[string][]Schema)

	yangMod, err := translib.GetYanglibInfo()

	if err != nil {
		glog.Warning("Unable to read yangmodules")
		return
	}

	YangModules.ModuleSetId = yangMod.ModuleSetId

	for module_key, module := range yangMod.Module {

		schema := Schema{}
		schema.Identifier = strings.ToLower(*module.Name)
		schema.Version = module_key.Revision
		schema.Format = "yang"
		schema.NameSpace = *module.Namespace
		schema.ModelPath = "/usr/models/yang/" + *module.Name + ".yang"
		schema.Location = "NETCONF"

		schemas = append(schemas, schema)

		YangSchemas[schema.Identifier] = append(YangSchemas[schema.Identifier], schema)

		mod := Module{}

		mod.Name = module.Name
		mod.Namespace = module.Namespace
		mod.Revision = module.Revision
		if module.Schema == nil {
			s := ("http://localhost/usr/models/yang/" + *module.Name)
			mod.Schema = &s
		} else {
			mod.Schema = module.Schema
		}
		if module.ConformanceType == 0 {
			mod.ConformanceType = "UNSET"
		} else if module.ConformanceType == 1 {
			mod.ConformanceType = "implement"
		} else if module.ConformanceType == 2 {
			mod.ConformanceType = "import"
		}

		YangModules.Modules = append(YangModules.Modules, mod)
	}
	read = true
}

func GetRequestHandler(context ssh.Context, rootNode *xmlquery.Node, target DbTarget) (string, error) {

	requests, err := ParseGetRequest(rootNode, false)

	if err != nil {
		return "", err
	}

	authenticator := context.Value("auth").(lib.Authenticator)

	for _, request := range requests {
		// Authorize
		if !authenticator.Authorize("get", request.path) {
			return "", errors.New(fmt.Sprintf("Unauthorized access %+s", request.path))
		}
		glog.Infof("Authorization passed %+s", request.path)
	}

	resultStr := "<data>"

	args := ""
	for _, request := range requests {

		pathResult, err := innerGetHandler(rootNode, request, target)

		if err != nil {
			return "", errors.New("Failed to handle request")
		}

		resultStr += pathResult
		args += request.path + ", "
	}

	// Account
	if !authenticator.Account("get", args) {
		return "", errors.New(fmt.Sprintf("Accounting failed get - args:%s", args))
	}

	glog.Infof("Accounting passed - get: %s", args)

	resultStr += "</data>"

	return resultStr, nil
}

func innerGetHandler(rootNode *xmlquery.Node, request GetRequest, target DbTarget) (string, error) {

	switch request.path {
	case "/modules-state:modules-state":
		response, err := xml.MarshalIndent(YangModules, "", "   ")
		if err != nil {
			return "", errors.New("Unable to read yang modules")
		}
		resStr := string(response)
		return resStr, nil
	case "/netconf-state:netconf-state/schemas":
		requests, _ := ParseGetRequest(rootNode, true)
		return getSchemas(requests[0].path), nil
	case "/operation:operation":
		return "", nil
	default:
		req := translib.GetRequest{Path: request.path, QueryParams: translib.QueryParameters{Content: string(target)}}
		resp, err1 := translib.Get(req)
		if err1 == nil {
			translibResponse := string(resp.Payload)

			// Check for empty response
			if translibResponse == "{}" {
				return "", nil
			}

			r := regexp.MustCompile("\"(.*?)\"")
			s := r.FindStringSubmatch(translibResponse)
			if len(s) == 0 {
				return "", errors.New("Translib parsing error [1]")
			}

			r2 := regexp.MustCompile("(\\S+):(\\S+)")
			s2 := r2.FindStringSubmatch(s[1])

			if len(s2) == 0 {
				return "", errors.New("Translib parsing error [2]")
			}

			translibResponse = strings.Replace(translibResponse, s2[0], s2[2], 1)

			if s2[1] != s2[2] {
				// Inner filters used
				translibResponse = "{\"" + s2[1] + "\":" + translibResponse + "}"
			}

			if len(request.filters) != 0 {
				glog.Infof("Filtering translib response %+s with filters %+v", translibResponse, request.filters)
				filteredResponse, err := filterJson(translibResponse, request)
				if err != nil {
					glog.Infof("Unable to filter response %+v", err)
					return "", errors.New("Unable to parse request [3]")
				}
				glog.Infof("Filtered response %+s", filteredResponse)
				translibResponse = filteredResponse
			}

			// Convert to xml
			jsonConv, _ := mxj.NewMapJson([]byte(translibResponse))
			conv := postChecks(rootNode, jsonConv)

			xmlPayload, _ := conv.Xml()

			xmlPayload = reorderKeys(request.path, xmlPayload)

			resultStr := string(xmlPayload)

			namespace := YangSchemas[s2[1]][0].NameSpace
			resultStr = strings.Replace(resultStr, s2[1], s2[1]+" xmlns=\""+namespace+"\"", 1)

			glog.V(0).Infof("[[DEBUG]] Begin inserting xmlns resultStr = %s", resultStr)
				r3 := regexp.MustCompile("<([\\w-]+?):([\\w-]+?)(/?)>")
				s3 := r3.FindAllStringSubmatch(resultStr, -1)
				for _, s := range s3 {
					prefix := s[1]
					name := s[2]
					slash := s[3]
					fmt.Printf("[[DEBUG]] Looking for namespace for prefix = %s\n", prefix)
					ns := getNamespace(prefix)
					fmt.Printf("[[DEBUG]] namespace for prefix = %s is %s\n", prefix, ns)
					resultStr = strings.ReplaceAll(resultStr, s[0], fmt.Sprintf("<%s:%s xmlns:%s=\"%s\"%s>", prefix, name, prefix, ns, slash))
				}

			glog.V(0).Infof("[[DEBUG]] End inserting xmlns resultStr = %s", resultStr)

			return resultStr, nil
		}
	}

	return "", nil
}

func getNamespace(prefix string) string {
	s, ok := YangSchemas[prefix]
	if !ok {
		return ""
	}
	if len(s) == 0 {
		return ""
	}
	return s[0].NameSpace
}

func postChecks(rootNode *xmlquery.Node, jsonConv mxj.Map) mxj.Map {

	filterNode := xmlquery.FindOne(rootNode, "//*[local-name()='filter']")
	containers := xmlquery.Find(filterNode, "./*")

	for _, modelContainer := range containers {

		for _, innerContainer := range xmlquery.Find(modelContainer, "./*") {

			for _, child := range xmlquery.Find(innerContainer, "./*") {

				exists, err := jsonConv.Exists(modelContainer.Data + "." + child.Data)

				if !exists || err != nil {
					// Not translib edge case, continue
					continue		
				}

				values, err := jsonConv.ValuesForPath(modelContainer.Data + "." + child.Data)

				if len(values) == 0 {
					glog.Infof("Empty response, restructuring skipped")
					continue
				}

				glog.Infof("Translib edge case handling encountered, restructuring response (%s) (%s)", modelContainer.Data, child.Data)
				glog.Infof("Pre conversion %+v", jsonConv)

				temp := jsonConv[modelContainer.Data]
				jsonConv[modelContainer.Data] = mxj.New()
				jsonConv[modelContainer.Data].(mxj.Map)[innerContainer.Data] = temp

				glog.Infof("Post conversion %+v", jsonConv)
			}

		}
	}

	return jsonConv
}

func filterJson(input string, request GetRequest) (string, error) {

	// Filters are always applied on lists

	glog.Infof("Filtering input %+s with filters %+v", input, request.filters)

	var i interface{}
    if err := json.Unmarshal([]byte(input), &i); err != nil {
        panic(err)
    }

	// This logic needs a refactor, o(n^5) horrible complexity
	for _, container := range i.(map[string]interface{}) {
		// Container level
		for listKey, list := range container.(map[string]interface{}){
			glog.Infof("List key %+v, list %+v", listKey, list)

			glog.Infof("Request path %+v", request.path)

			r := regexp.MustCompile("/.*/.*/(\\w+)")
			s := r.FindStringSubmatch(request.path)

			if len(s) == 0 {
				return "", errors.New("Failed to get list name")
			}

			listName := s[1]

			if listName == listKey {
				// This list has a filter, apply it.
				arr, ok := list.([]interface{})
				if ok {
					for _, arrObj := range arr {

						// Iterate over each element of the obj to see if we need to filter it
						aa := arrObj.(map[string]interface{})

						for k, _ := range aa {
							glog.Infof("Reorder check %+s %+s", aa , k)
							// Check if the key exist in the filter, if so keep it. Delete anything else
							if !contains(request.filters, k) {
								delete(aa, k)
							}
						}
					}
				}
			}
		}
	}

    output, err := json.Marshal(i)
    if err != nil {
        panic(err)
    }

	return string(output), nil
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func reorderKeys(path string, xml []byte) []byte {

	arr, _ := mxj.NewMapXmlSeq(xml)

	for k, keys := range netconf_codegen.SonicMap {

		var keysReversed = make([]string, len(keys))

		for i, k := range keys {
			keysReversed[len(keys)-i-1] = k
		}

		if strings.Contains(k, path+"/") || strings.Contains(path+"/", k) {

			pathSplit := strings.Split(k, "/")

			module := strings.Split(pathSplit[1], ":")[0]
			parent := pathSplit[2]
			list := pathSplit[3]

			if _, ok := arr[module].(map[string]interface{})[parent]; ok {
				
				// Main container request

				listObj := arr[module].(map[string]interface{})[parent].(map[string]interface{})[list]

				switch listObj.(type) {
					case map[string]interface{}:

						listCast := arr[module].(map[string]interface{})[parent].(map[string]interface{})[list].(map[string]interface{})

						for i, key := range keysReversed {
							// We must check because in lists with 2 or more keys, if filtering is applied, some keys will not be in listCast
							if val, ok := listCast[key]; ok {
								val.(map[string]interface{})["#seq"] = -(i + 1)
							}
						}

						arr[module].(map[string]interface{})[parent].(map[string]interface{})[list] = listCast

					case []interface{}:

						listArr := arr[module].(map[string]interface{})[parent].(map[string]interface{})[list].([]interface{})

						for i, value := range listArr {

							listItem := value.(map[string]interface{})

							for i, key := range keysReversed {
								// We must check because in lists with 2 or more keys, if filtering is applied, some keys will not be in listCast
								if val, ok := listItem[key]; ok {
									val.(map[string]interface{})["#seq"] = -(i + 1)
								}
							}

							listArr[i] = listItem
						}

						arr[module].(map[string]interface{})[parent].(map[string]interface{})[list] = listArr
				}

			}
		}
	}

	result, _ := arr.Xml()
	return result
}

func getSchemas(xpath string) string {
	xpath = strings.ToLower(xpath)
	var netconf_state State
	var temp Schema

	if xpath == RPCGetSchemas || xpath == RPCGetSchemas+"/schema" {
		for _, schemas := range YangSchemas {
			netconf_state.Schemas = append(netconf_state.Schemas, schemas...)
		}
		return prepareSchemasReply(netconf_state)
	}

	for _, schemas := range YangSchemas {
		for _, schema := range schemas {
			temp = Schema{}

			if strings.Contains(xpath, "identifier") {
				temp.Identifier = schema.Identifier
			}

			if strings.Contains(xpath, "version") {
				temp.Version = schema.Version
			}

			if strings.Contains(xpath, "format") {
				temp.Format = schema.Format
			}

			if strings.Contains(xpath, "namespace") {
				temp.NameSpace = schema.NameSpace
			}

			if strings.Contains(xpath, "location") {
				temp.Location = schema.Location
			}

			netconf_state.Schemas = append(netconf_state.Schemas, temp)
		}
	}

	return prepareSchemasReply(netconf_state)
}

func FilterSchemaHandler(rootNode *xmlquery.Node) (string, error) {

	req, err := ParseGetSchemaRequest(rootNode)

	if err != nil {
		return "", err
	}

	schema := Schema{}

	identifier := strings.ToLower(req.Identifier)
	format := strings.ToLower(req.Format)

	for _, model := range YangSchemas[identifier] {
		if (req.Format == "" || model.Format == format) &&
			(req.Version == "" || model.Version == req.Version) {
			schema = model
			break
		}
	}

	yangData := readYangFile(schema.ModelPath)
	yangData = html.EscapeString(yangData)

	yangData = "<data xmlns=\"urn:ietf:params:xml:ns:yang:ietf-netconf-monitoring\">" + yangData + "</data>"

	return yangData, nil
}

func readYangFile(path string) string {
	yangFile, err := os.Open(path)
	if err != nil {
		fmt.Println(err)
	}

	defer yangFile.Close()

	byteValue, _ := ioutil.ReadAll(yangFile)

	return html.EscapeString(string(byteValue))
}

func prepareSchemasReply(st State) string {
	response, _ := xml.MarshalIndent(st, "", "   ")
	return string(response)
}

func EditRequestHandler(context ssh.Context, rootNode *xmlquery.Node) (string, error) {

	exists, err := redisClient.Exists("CONFIG_LOCK").Result()

	if err != nil {
		return "", err
	}

	lockId, err := redisClient.Get("CONFIG_LOCK").Result()

	glog.Infof("Datastore lock info exists %+v, lock %+v, err %+v", exists, lockId, err)

	if exists == 1 && lockId != context.Value("uuid") {
		// There is a lock, and it is not owned by current session
		return "", errors.New("Datastore locked")
	}

	configs, err := ParseEditRequest(rootNode)

	glog.Infof("Configs extracted %+v", configs)

	if err != nil {
		return "", err
	}

	authenticator := context.Value("auth").(lib.Authenticator)

	for _, config := range configs {
		// Authorize
		if !authenticator.Authorize("edit-config", config.path) {
			return "", errors.New(fmt.Sprintf("Unauthorized access %s", config.path))
		}
		glog.Infof("Authorization passed %s", config.path)
	}

	for _, config := range configs {

		jsonStr, err := json.Marshal(config.payload)

		if err != nil {
			return "", errors.New("Converting payload to byte array failed")
		}

		req := translib.SetRequest{Path: config.path, Payload: jsonStr}

		switch config.operation {
		case "merge":
			_, err = translib.Create(req)
		case "create":
			_, err = translib.Create(req)
		case "remove":
			_, err = translib.Delete(req)
			if err != nil && err.Error() == "Resource not found" {
				err = nil				
			}
		case "replace":
			_, err = translib.Replace(req)
		case "delete":
			_, err = translib.Delete(req)
		default:
			return "", errors.New("unsupported operation: " + config.operation)
		}

		if err != nil {
			return "", err
		}

		// Account
		if !authenticator.Account("edit-config", config.path) {
			return "", errors.New(fmt.Sprintf("Accounting failed edit-config - args:%s", config.path))
		}

		glog.Infof("Accounting passed - edit-config: %s", config.path)
		
	}

	return "ok", nil
}

func lockRequestHandler(context ssh.Context, rootNode *xmlquery.Node) (string, error) {

	// Parser to get Target node
	targetNode := xmlquery.FindOne(rootNode, "//*[local-name() = 'target']/*")
	if targetNode == nil {
		return "", errors.New("target store unsepecified")
	}

	if targetNode.Data != "running" {
		return "", errors.New("Target must be running config")
	}

	// lockDuration := 10 // default

	// durationNode := xmlquery.FindOne(rootNode, "//*[local-name() = 'duration']/*")

	// glog.Infof("Duration node", durationNode)
	// glog.Infof("duration input", durationNode.Data)

	// if durationNode != nil {
	// 	inputduration, err := strconv.Atoi(durationNode.Data)

	// 	if err != nil {
	// 		return "", errors.New("Unable to parse duration")
	// 	}

	// 	lockDuration = inputduration
	// }

	authenticator := context.Value("auth").(lib.Authenticator)

	if !authenticator.Authorize("lock", "") {
		return "", errors.New(fmt.Sprintf("Unauthorized access %+s", "lock"))
	}

	glog.Infof("Authorization passed %+s", "lock")

	lockAcquired, _ := redisClient.SetNX("CONFIG_LOCK", context.Value("uuid"), 15 * time.Second).Result()

	if !lockAcquired {
		return "", errors.New("Lock failed, lock is already held")
	}

	resultStr := "ok"

	// Account
	if !authenticator.Account("get", "") {
		return "", errors.New(fmt.Sprintf("Accounting failed lock - args:%s", ""))
	}

	glog.Infof("Accounting passed - lock: %s", "")

	return resultStr, nil
}

func unlockRequestHandler(context ssh.Context, rootNode *xmlquery.Node) (string, error) {
	// Parser to get Target node
	targetNode := xmlquery.FindOne(rootNode, "//*[local-name() = 'target']/*")
	if targetNode == nil {
		return "", errors.New("target store unsepecified")
	}

	if targetNode.Data != "running" {
		return "", errors.New("Target must be running config")
	}

	authenticator := context.Value("auth").(lib.Authenticator)

	if !authenticator.Authorize("unlock", "") {
		return "", errors.New(fmt.Sprintf("Unauthorized access %+s", "unlock"))
	}

	glog.Infof("Authorization passed %+s", "lock")

	lock, err := redisClient.Get("CONFIG_LOCK").Result()

	if err == redis.Nil {
		return "", errors.New("No active lock")
	}

	if err != nil {
		return "", errors.New("Unhandled redis error")
	}

	if lock != context.Value("uuid") {
		return "", errors.New("Current session doesn't own active lock")
	}

	redisClient.Del("CONFIG_LOCK")

	// Account
	if !authenticator.Account("unlock", "") {
		return "", errors.New(fmt.Sprintf("Accounting failed unlock - args:%s", ""))
	}

	glog.Infof("Accounting passed - unlock: %s", "")

	return "ok", nil
}

func commitRequestHandler(context ssh.Context, rootNode *xmlquery.Node) (string, error){

	authenticator := context.Value("auth").(lib.Authenticator)

	if !authenticator.Authorize("commit", "") {
		return "", errors.New(fmt.Sprintf("Unauthorized access %s", "commit"))
	}

	glog.Infof("Authorization passed %s", "commit")

	err := saveConfig()

	if err != nil {
		return "", errors.New("Unable to commit changes made, configuration will not persist")
	}

	if !authenticator.Account("commit", "") {
		return "", errors.New(fmt.Sprintf("Accounting failed cmd:%s", "commit"))
	}

	glog.Infof("Accounting passed - %s", "commit")

	return "ok", nil
}

func saveConfig() error {

	// Config increment checks
	exists, err := redisClient.Exists("VERSIONS|CONFIGURATION").Result()

	if err == nil && exists == 1 {
		
		result, err := redisClient.HGetAll("VERSIONS|CONFIGURATION").Result()

		if version, ok := result["VERSION"]; err == nil && ok {

			version, err := strconv.Atoi(version)

			if err == nil {

				newVersion := version + 1

				_, err = redisClient.HSet("VERSIONS|CONFIGURATION", "VERSION", newVersion).Result()

				if err == nil {
					glog.Infof("Updated configuration version from %d to %d", version , newVersion)
				}

			}

		}
	}

	args := []string{"-c", "$(sonic-cfggen -d --print-data > /etc/sonic/config_db.json)"}
	_, err = exec.Command("bash", args...).Output()

	if err != nil {
		glog.Error("Config save failed, configuration will not persist")
		return err
	}

	glog.Infof("Configuration saved");

	return nil
}

