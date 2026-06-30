package server

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"orange/sonic-netconf-server/build/netconf_codegen"

	"github.com/antchfx/xmlquery"
	"github.com/golang/glog"
)

type Config struct {
	path      string
	operation string
	payload   map[string]interface{}
	keys      int
}

type GetRequest struct {
	path 		string
	filters 	[]string
}

type RPCRequest struct {
	path    string
	payload map[string]interface{}
}

// RFC6241 6.2.3. A node that contains child elements is a containment node.
func isContainmentNode(node *xmlquery.Node) bool {
	return len(xmlquery.Find(node, "./*")) > 0
}

func getTextOfNode(node *xmlquery.Node) string {
	ret := ""
	for _, text := range xmlquery.Find(node, "./text()") {
		ret += text.Data
	}
	return ret
}

// RFC6241 6.2.4. An empty leaf node is a selection node.
func isSelectionNode(node *xmlquery.Node) bool {
	if isContainmentNode(node) {
		return false
	}
	return strings.TrimSpace(getTextOfNode(node)) == ""
}

// RFC6241 6.2.5. A leaf node with simple content is a content-match node — predicate filter.
func isContentMatchNode(node *xmlquery.Node) bool {
	if isContainmentNode(node) {
		return false
	}
	return strings.TrimSpace(getTextOfNode(node)) != ""
}

func tagName(node *xmlquery.Node) string {
	if node.Type != xmlquery.ElementNode {
		return ""
	} else if node.Prefix == "" {
		return node.Data
	} else {
		return node.Prefix + ":" + node.Data
	}
}

func getAttrValue(node *xmlquery.Node, name string) *string {
	for _, a := range node.Attr {
		if a.Name.Local == name {
			v := a.Value
			return &v
		}
	}
	return nil
}

func parseContainmentNode(self *xmlquery.Node, pathPrefix string) []string {
	children := xmlquery.Find(self, "./*")
	pathPrefix += "/" + tagName(self)

	conditions := []string{}
	for _, child := range children {
		if isContentMatchNode(child) {
			key := tagName(child)
			value := strings.TrimSpace(getTextOfNode(child))
			conditions = append(conditions, fmt.Sprintf("%s=%s", key, value))
		}
	}
	if len(conditions) > 0 {
		pathPrefix += "[" + strings.Join(conditions, " and ") + "]"
	}

	paths := []string{}
	for _, child := range children {
		if isContainmentNode(child) {
			paths = append(paths, parseContainmentNode(child, pathPrefix)...)
		}
	}
	for _, child := range children {
		if isSelectionNode(child) {
			paths = append(paths, pathPrefix+"/"+tagName(child))
		}
	}
	if len(paths) == 0 {
		paths = append(paths, pathPrefix)
	}
	return paths
}

// ParseGetRequest walks the <filter> subtree and converts it into translib xpaths.
// The recursive form handles arbitrary depth, content-match-node predicates, and
// namespace-prefixed element names. The appendAll parameter is retained for caller
// compatibility but is no longer consulted — the recursive walk emits every path.
func ParseGetRequest(node *xmlquery.Node, appendAll bool) ([]GetRequest, error) {
	_ = appendAll

	glog.Info("Starting parse Get request")

	filterNode := xmlquery.FindOne(node, "//*[local-name()='filter']")
	if filterNode == nil {
		return []GetRequest{}, errors.New("Need filter element, can't return all configuration for now")
	}

	if debugXPath := getAttrValue(filterNode, "debugxpath"); debugXPath != nil {
		return []GetRequest{{path: *debugXPath, filters: []string{}}}, nil
	}

	queryPaths := []GetRequest{}
	for _, modelContainer := range xmlquery.Find(filterNode, "./*") {
		glog.V(0).Infof("Parsing for model %s started", tagName(modelContainer))
		for _, path := range parseContainmentNode(modelContainer, "") {
			queryPaths = append(queryPaths, GetRequest{path: path, filters: []string{}})
		}
		glog.V(0).Infof("Parsing for model %s ended", modelContainer.Data)
	}

	glog.Infof("Paths found in get request %+v", queryPaths)
	return queryPaths, nil
}

func ParseGetSchemaRequest(node *xmlquery.Node) (GetSchema, error) {
	identifier := xmlquery.FindOne(node, "//identifier/text()")
	format := xmlquery.FindOne(node, "//format/text()")
	version := xmlquery.FindOne(node, "//version/text()")

	s := GetSchema{}
	if identifier == nil {
		return s, errors.New("Identifier not passed")
	}

	s.Identifier = identifier.Data

	if format != nil {
		s.Format = format.Data
	}

	if version != nil {
		s.Version = version.Data
	}

	return s, nil
}

func ParseEditRequest(node *xmlquery.Node) ([]Config, error) {

	// get target
	targetNode := xmlquery.FindOne(node, "//*[local-name() = 'target']/*")
	if targetNode == nil {
		return []Config{}, errors.New("target store unsepecified")
	}

	if targetNode.Data != "running" {
		return []Config{}, errors.New("Target must be running config")
	}

	var configs []Config

	deleteRequest, err := extractAtomicRequestsByOpTag2(node, "delete")
	if err == nil {
		configs = append(configs, deleteRequest...)
	}

	removeRequest, err := extractAtomicRequestsByOpTag2(node, "remove")
	if err == nil {
		configs = append(configs, removeRequest...)
	}

	mergeRequest, err := extractTransactionalRequestByOpTag2(node, "merge")
	if err == nil {
		configs = append(configs, mergeRequest...)
	}

	replaceRequest, err := extractTransactionalRequestByOpTag3(node, "replace")
	if err == nil {
		configs = append(configs, replaceRequest...)
	}

	createRequest, err := extractTransactionalRequestByOpTag2(node, "create")
	if err == nil {
		configs = append(configs, createRequest...)
	}

	glog.Infof("configs", configs)

	return configs, nil
}

func ParseRPCRequest(node *xmlquery.Node) ([]RPCRequest, error) {

	var requests []RPCRequest

	for _, rpcContainer := range xmlquery.Find(node, "//*[local-name() = 'sonic-rpc']/*") {

		rpc := strings.TrimSpace(rpcContainer.Data)
		payload := make(map[string]interface{})

		for _, leaf := range xmlquery.Find(rpcContainer, "./*") {

			leafTag := strings.TrimSpace(leaf.Data)
			leafTextNode := xmlquery.FindOne(leaf, "text()")
			leafText := getInnerText(leafTextNode)

			payload[leafTag] = leafText
		}

		formattedPayload := make(map[string]interface{})
		formattedPayload["sonic-rpc:input"] = payload

		rpcRequest := RPCRequest{
			path:    "/sonic-rpc:" + rpc,
			payload: formattedPayload,
		}

		requests = append(requests, rpcRequest)
	}

	return requests, nil
}

func extractTransactionalRequestByOpTag2(node *xmlquery.Node, opTag string) ([]Config, error) {

	var configs []Config

	containers := xmlquery.Find(node, "//*[local-name() = 'config']/*")

	for _, modelContainer := range containers {

		var config Config
		config.operation = opTag
		config.payload = make(map[string]interface{})

		config.path = "/" + modelContainer.Data + ":" + modelContainer.Data //translib path building

		// Handle inner container
		for _, innerContainer := range xmlquery.Find(modelContainer, "./*") {
	
			// Handle outer container
			innerContainerPayload := make(map[string][]interface{})

			// Handle each "list" inside innerContainer
			lists := xmlquery.Find(innerContainer, "./*")

			for _, list := range lists {

				listOp := getOperation(list, "merge")

				
				listPath := config.path + "/" + innerContainer.Data + "/" + list.Data
				var leafListSet map[string]bool
				if strings.Contains(listPath, "sonic") {
					leafListSet = netconf_codegen.SonicLeafListFields[listPath]
				} else {
					leafListSet = netconf_codegen.CommonLeafListFields[listPath]
				}

				// Check leafs for atomic operations
				leafs := xmlquery.Find(list, "./*")

				listItemData := make(map[string]interface{})

				for _, leaf := range leafs {

					leafTextNode := xmlquery.FindOne(leaf, "text()")
					leafText := getInnerText(leafTextNode)
					leafTag := strings.TrimSpace(leaf.Data)

					leafOp := getOperation(leaf, "n/a")
					if leafOp == "n/a" {
						leafOp = listOp
					}

					if leafOp != opTag {
						continue
					}

					// quotes means we want this leaf to be a string
					str, ok := leafText.(string)
					if ok && strings.Contains(str, "\"") {
						leafText, _ = strconv.Unquote(str)
					}

					
					if leafListSet[leafTag] {
						if existing, exists := listItemData[leafTag]; exists {
							listItemData[leafTag] = append(existing.([]interface{}), leafText)
						} else {
							listItemData[leafTag] = []interface{}{leafText}
						}
					} else {
						listItemData[leafTag] = leafText
					}
				}

				if len(listItemData) != 0 {
					innerContainerPayload[list.Data] = append(innerContainerPayload[list.Data], listItemData)
				}
			}

			if len(innerContainerPayload) != 0 {
				config.payload[modelContainer.Data+":"+innerContainer.Data] = innerContainerPayload
			}
		}

		if len(config.payload) != 0 {
			configs = append(configs, config)
		}
	}

	return configs, nil
}

func extractTransactionalRequestByOpTag3(node *xmlquery.Node, opTag string) ([]Config, error) {

	var configs []Config

	containers := xmlquery.Find(node, "//*[local-name() = 'config']/*")

	for _, modelContainer := range containers {

		var config Config
		config.operation = opTag
		config.payload = make(map[string]interface{})

		config.path = "/" + modelContainer.Data + ":" + modelContainer.Data //translib path building

		// Handle inner container
		for _, innerContainer := range xmlquery.Find(modelContainer, "./*") {
	
			// Handle outer container
			innerContainerPayload := make(map[string][]interface{})

			// Handle each "list" inside innerContainer
			lists := xmlquery.Find(innerContainer, "./*")

			for _, list := range lists {

				listOp := getOperation(list, "merge")

				
				listPath := config.path + "/" + innerContainer.Data + "/" + list.Data
				var leafListSet map[string]bool
				if strings.Contains(listPath, "sonic") {
					leafListSet = netconf_codegen.SonicLeafListFields[listPath]
				} else {
					leafListSet = netconf_codegen.CommonLeafListFields[listPath]
				}

				// Check leafs for atomic operations
				leafs := xmlquery.Find(list, "./*")

				listItemData := make(map[string]interface{})

				for _, leaf := range leafs {

					leafTextNode := xmlquery.FindOne(leaf, "text()")
					leafText := getInnerText(leafTextNode)
					leafTag := strings.TrimSpace(leaf.Data)

					leafOp := getOperation(leaf, "n/a")
					if leafOp == "n/a" {
						leafOp = listOp
					}

					if leafOp != opTag {
						continue
					}

					// quotes means we want this leaf to be a string
					str, ok := leafText.(string)
					if ok && strings.Contains(str, "\"") {
						leafText, _ = strconv.Unquote(str)
					}

					
					if leafListSet[leafTag] {
						if existing, exists := listItemData[leafTag]; exists {
							listItemData[leafTag] = append(existing.([]interface{}), leafText)
						} else {
							listItemData[leafTag] = []interface{}{leafText}
						}
					} else {
						listItemData[leafTag] = leafText
					}
				}

				if len(listItemData) != 0 {
					innerContainerPayload[list.Data] = append(innerContainerPayload[list.Data], listItemData)
				}
			}

			if len(innerContainerPayload) != 0 {
				config.payload[modelContainer.Data+":"+modelContainer.Data] = make(map[string]interface{})
				config.payload[modelContainer.Data+":"+modelContainer.Data].(map[string]interface{})[innerContainer.Data] = innerContainerPayload
			}
		}

		if len(config.payload) != 0 {
			configs = append(configs, config)
		}
	}

	return configs, nil
}

func extractAtomicRequestsByOpTag2(node *xmlquery.Node, opTag string) ([]Config, error) {

	containers := xmlquery.Find(node, "//*[local-name() = 'config']/*")

	var configs []Config

	for _, modelContainer := range containers {

		// Handle outer container
		basePath := "/" + modelContainer.Data + ":" + modelContainer.Data //translib path building

		// Handler inner containers
		for _, innerContainer := range xmlquery.Find(modelContainer, "./*") {

			if innerContainer == nil {
				continue
			}

			path := basePath + "/" + innerContainer.Data

			// Handle each "list" inside innerContainer
			lists := xmlquery.Find(innerContainer, "./*")

			for _, list := range lists {

				innerPath := path + "/" + list.Data
				listOp := getOperation(list, "merge")

				var keys []string

				if strings.Contains(innerPath, "sonic") {
					keys = netconf_codegen.SonicMap[innerPath]
				} else {
					keys = netconf_codegen.CommonMap[innerPath]
				}

				// update path with keys for list
				for _, key := range keys {
					keyNode := xmlquery.FindOne(list, "//*[local-name() = '"+key+"']/text()")
					if keyNode != nil {
						innerPath += "[" + key + "=" + fmt.Sprintf("%v", strings.TrimSpace(keyNode.Data)) + "]"
					}
				}

				if listOp == opTag {
					// Append update list for keys
					var leafConfig Config
					leafConfig.operation = listOp
					leafConfig.path = innerPath
					leafConfig.payload = make(map[string]interface{})
					leafConfig.keys = len(keys)
					configs = append(configs, leafConfig)
				}

				// Check leafs for atomic operations
				leafs := xmlquery.Find(list, "./*")

				for _, leaf := range leafs {

					leafTag := strings.TrimSpace(leaf.Data)
					leafPath := innerPath + "/" + leafTag

					leafTextNode := xmlquery.FindOne(leaf, "text()")
					leafText := getInnerText(leafTextNode)

					isKey := false

					// Check if leaf is key to not skip its config
					for _, key := range keys {
						if key == leafTag {
							isKey = true
						}
					}

					if isKey {
						continue
					}

					leafOp := getOperation(leaf, "n/a")
					if leafOp == "n/a" {
						leafOp = listOp
					}

					if leafOp != opTag {
						continue
					}

					var leafConfig Config
					leafConfig.operation = opTag
					leafConfig.path = leafPath
					leafConfig.payload = make(map[string]interface{})
					leafConfig.payload[modelContainer.Data+":"+leafTag] = leafText
					leafConfig.keys = len(keys)
					configs = append(configs, leafConfig)
				}
			}
		}
	}

	sort.Slice(configs, func(i, j int) bool {
		return configs[i].keys > configs[j].keys
	})

	return configs, nil
}

func getOperation(node *xmlquery.Node, defaultVal string) string {

	for _, s := range node.Attr {
		if s.Name.Local == "operation" {
			return s.Value
		}
	}

	return defaultVal
}

func getInnerText(node *xmlquery.Node) interface{} {

	if node != nil {

		val := strings.TrimSpace(node.Data)

		// No conversion
		if node.Parent.SelectAttr("conversion") == "none" {
			return val
		}

		// try converting to float
		float, err := strconv.ParseUint(val, 10, 64)
		if err == nil {
			return float
		}

		// try boolean conversion
		boolean, err := strconv.ParseBool(val)
		if err == nil {
			return boolean
		}

		return val
	}

	return ""
}

func appendParamsToPath(path string, params map[string]interface{}, appendAll bool) string {

	if appendAll {
		for k, val := range params {
			path += "/[" + k + "=" + fmt.Sprintf("%v", val) + "]"
		}
		return path
	}

	var keys []string

	if strings.Contains(path, "sonic") {
		keys = netconf_codegen.SonicMap[path]
	} else {
		keys = netconf_codegen.CommonMap[path]
	}

	for _, key := range keys {
		if val, ok := params[key]; ok {
			path += "[" + key + "=" + fmt.Sprintf("%v", val) + "]"
		} else {
			panic("key for path not given")
		}
	}

	return path
}

func buildPath(node *xmlquery.Node, path string) string {

	if node == nil {
		return path
	}

	textNode := xmlquery.FindOne(node, "text()")

	if textNode == nil || strings.TrimSpace(textNode.Data) == "" {
		path += "/" + node.Data
	}

	return buildPath(xmlquery.FindOne(node, "./*"), path)
}

func getParameters(node *xmlquery.Node) map[string]interface{} {
	return _getParameters(node, make(map[string]interface{}))
}

func _getParameters(node *xmlquery.Node, params map[string]interface{}) map[string]interface{} {

	if node == nil {
		return params
	}

	children := xmlquery.Find(node, "./*")

	for _, c := range children {

		textChildren := xmlquery.Find(c, "text()")

		for _, tc := range textChildren {

			if strings.TrimSpace(tc.Data) == "" {
				continue
			}

			val := strings.TrimSpace(tc.Data)

			if tc.Parent.SelectAttr("conversion") == "none" {
				params[c.Data] = val
				continue
			}

			// try converting to float
			float, err := strconv.ParseFloat(val, 64)
			if err == nil {
				params[c.Data] = float
				continue
			}

			// try boolean conversion
			boolean, err := strconv.ParseBool(val)
			if err == nil {
				params[c.Data] = boolean
				continue
			}

			params[c.Data] = val
		}

	}

	return _getParameters(xmlquery.FindOne(node, "./*"), params)
}
