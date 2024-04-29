//
// Software Name: sonic-netconf-server
// SPDX-FileCopyrightText: Copyright (c) Orange SA
// SPDX-License-Identifier: Apache 2.0
//
// This software is distributed under the Apache 2.0 licence,
// the text of which is available at https://opensource.org/license/apache-2-0/
// or see the "LICENSE" file for more details.
//
// Authors: hossam4.hassan@orange.com, abdelmuhaimen.seaudi@orange.com
// Software description: RFC compliant NETCONF server implementation for SONiC
//

package server

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"orange/sonic-netconf-server/build/netconf_codegen"

	"github.com/Azure/sonic-mgmt-common/translib"
	"github.com/antchfx/xmlquery"
	"github.com/clbanning/mxj/v2"
	"github.com/go-redis/redis/v7"
	"github.com/golang/glog"
)

var (
	YangSchemas map[string][]Schema
	YangModules ModulesState
	yangModulesInit	= false
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

	yangModulesInit = true
}

func GetRequestHandler(authenticator Authenticator, rootNode *xmlquery.Node) (string, error) {

	requests, err := ParseGetRequest(rootNode)

	glog.Infof("Extracted requests %+v", requests)

	if err != nil {
		return "", err
	}

	// authenticator := context.Value("auth").(Authenticator)

	for _, request := range requests {
		// Authorize
		if !authenticator.Authorize("get", request.path) {
			return "", errors.New(fmt.Sprintf("[AUTH] Unauthorized access %+s", request.path))
		}
		glog.Infof("[AUTH] authorization passed %+s", request.path)
	}

	resultStr := "<data>"

	args := ""
	for _, request := range requests {

		pathResult, err := innerGetHandler(rootNode, request)

		if err != nil {
			return "", errors.New("Failed to handle request")
		}

		resultStr += pathResult
		args += request.path + ", "
	}

	// Account
	if !authenticator.Account("get", args) {
		return "", errors.New(fmt.Sprintf("[AUTH] Accounting failed get - args:%s", args))
	}

	glog.Infof("[AUTH] Accounting passed - get: %s", args)

	resultStr += "</data>"

	return resultStr, nil
}

func innerGetHandler(rootNode *xmlquery.Node, request GetRequest) (string, error) {

	switch request.path {
	case "/modules-state:modules-state":
		response, err := xml.MarshalIndent(YangModules, "", "   ")
		if err != nil {
			return "", errors.New("Unable to read yang modules")
		}
		return string(response), nil
	case "/netconf-state:netconf-state/schemas":
		requests, _ := ParseGetRequest(rootNode)
		return getSchemas(requests[0].path), nil
	case "/operation:operation":
		return "", nil
	default:
		req := translib.GetRequest{Path: request.path}
		resp, err1 := translib.Get(req)
		if err1 == nil {

			//TODO: This section needs refactoring, post-translib get request glue code

			translibResponse := string(resp.Payload)

			// Check for empty response
			if translibResponse == "{}" {
				return "", nil
			}

			// Ensure correct translib output
			r := regexp.MustCompile("\"(.*?)\"")
			s := r.FindStringSubmatch(translibResponse)
			if len(s) == 0 {
				return "", errors.New("Translib parsing error [1]")
			}

			// Extract module container info
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
				glog.V(0).Infof("Filtering translib response %+s with filters %+v", translibResponse, request.filters)
				filteredResponse, err := filterJson(translibResponse, request)
				if err != nil {
					glog.V(0).Infof("Unable to filter response %+v", err)
					return "", errors.New("Unable to parse request [3]")
				}
				glog.V(0).Infof("Filtered response %+s", filteredResponse)
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

			return resultStr, nil
		}
	}

	return "", nil
}

func postChecks(rootNode *xmlquery.Node, jsonConv mxj.Map) mxj.Map {

	filterNode := xmlquery.FindOne(rootNode, "//filter")
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
					glog.V(0).Infof("Empty response, restructuring skipped")
					continue
				}

				glog.V(0).Infof("Translib edge case handling encountered, restructuring response (%s) (%s)", modelContainer.Data, child.Data)

				glog.V(0).Infof("Pre conversion %+v", jsonConv)

				temp := jsonConv[modelContainer.Data]
				jsonConv[modelContainer.Data] = mxj.New()
				jsonConv[modelContainer.Data].(mxj.Map)[innerContainer.Data] = temp

				glog.V(0).Infof("Post conversion %+v", jsonConv)
			}

		}
	}

	return jsonConv
}

func filterJson(input string, request GetRequest) (string, error) {

	// Filters are always applied on lists

	glog.V(0).Infof("Filtering input %+s with filters %+v", input, request.filters)

	var i interface{}
    if err := json.Unmarshal([]byte(input), &i); err != nil {
        return "", err
    }

	// This logic needs a refactor, o(n^4) horrible complexity
	for _, container := range i.(map[string]interface{}) {
		// Container level
		for listKey, list := range container.(map[string]interface{}){
			glog.V(0).Infof("List key %+v, list %+v", listKey, list)

			r := regexp.MustCompile("/.*/.*/(\\S+)")
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
							glog.V(0).Infof("Reorder check %+s %+s", aa , k)
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
        return "", err
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

func GetSchemaHandler(rootNode *xmlquery.Node) (string, error) {

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

func Reverse(s []string) []string {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}
