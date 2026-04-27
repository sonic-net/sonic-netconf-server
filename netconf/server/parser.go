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
	path    string
	filters []string
}

type RPCRequest struct {
	path    string
	payload map[string]interface{}
}

func ParseCreateSubscription(node *xmlquery.Node) (CreateSubscription, []string, error) {
	sub := CreateSubscription{}

	subNode := xmlquery.FindOne(node, "//*[local-name() = 'create-subscription']")
	if subNode == nil {
		return sub, nil, errors.New("missing create-subscription")
	}
	if subNode.NamespaceURI != "" && subNode.NamespaceURI != NsNetconfNotification {
		return sub, nil, errors.New("invalid create-subscription namespace")
	}

	if streamNode := xmlquery.FindOne(subNode, "./*[local-name() = 'stream']/text()"); streamNode != nil {
		sub.Stream = strings.TrimSpace(streamNode.Data)
	}
	if startNode := xmlquery.FindOne(subNode, "./*[local-name() = 'startTime']/text()"); startNode != nil {
		sub.StartTime = strings.TrimSpace(startNode.Data)
	}
	if stopNode := xmlquery.FindOne(subNode, "./*[local-name() = 'stopTime']/text()"); stopNode != nil {
		sub.StopTime = strings.TrimSpace(stopNode.Data)
	}

	filterNode := xmlquery.FindOne(subNode, "./*[local-name() = 'filter']")
	if filterNode == nil {
		return sub, nil, nil
	}
	sub.HasFilter = true

	sub.Filter.Type = filterNode.SelectAttr("type")
	sub.Filter.Select = filterNode.SelectAttr("select")
	sub.Filter.Subtree = strings.TrimSpace(filterNode.OutputXML(false))

	paths, err := subscriptionFilterPaths(filterNode)
	if err != nil {
		return sub, nil, err
	}

	return sub, paths, nil
}

func subscriptionFilterPaths(filterNode *xmlquery.Node) ([]string, error) {
	filterType := strings.TrimSpace(filterNode.SelectAttr("type"))
	if filterType == "" {
		filterType = "subtree"
	}

	switch filterType {
	case "xpath":
		selectAttr := strings.TrimSpace(filterNode.SelectAttr("select"))
		if selectAttr == "" {
			return nil, errors.New("missing xpath select")
		}
		if !isSimpleSubscriptionXPath(selectAttr) {
			return nil, errors.New("unsupported xpath filter")
		}
		return []string{selectAttr}, nil
	case "subtree":
		return subscriptionSubtreePaths(filterNode)
	default:
		return nil, errors.New("unsupported filter type")
	}
}

func subscriptionSubtreePaths(filterNode *xmlquery.Node) ([]string, error) {
	var paths []string

	for _, modelContainer := range xmlquery.Find(filterNode, "./*") {
		basePath := "/" + modelContainer.Data
		if len(xmlquery.Find(modelContainer, "./*")) == 0 {
			paths = append(paths, basePath)
			continue
		}
		childPaths, err := subscriptionChildPaths(modelContainer, basePath)
		if err != nil {
			return nil, err
		}
		paths = append(paths, childPaths...)
	}

	return dedupeStrings(paths), nil
}

func subscriptionChildPaths(node *xmlquery.Node, currentPath string) ([]string, error) {
	children := xmlquery.Find(node, "./*")
	if len(children) == 0 {
		return []string{currentPath}, nil
	}

	var paths []string
	keySet := subscriptionKeySet(currentPath)
	for _, child := range children {
		if _, isKey := keySet[child.Data]; isKey {
			continue
		}

		childPath := currentPath + "/" + child.Data
		childPath = appendSubscriptionKeys(child, childPath)

		grandChildren := xmlquery.Find(child, "./*")
		if len(grandChildren) == 0 {
			if hasDirectText(child) {
				return nil, errors.New("subtree filter values are not supported")
			}
			paths = append(paths, childPath)
			continue
		}
		childPaths, err := subscriptionChildPaths(child, childPath)
		if err != nil {
			return nil, err
		}
		paths = append(paths, childPaths...)
	}
	if len(paths) == 0 {
		paths = append(paths, currentPath)
	}
	return paths, nil
}

func appendSubscriptionKeys(node *xmlquery.Node, path string) string {
	keys := subscriptionKeys(path)
	for _, key := range keys {
		keyNode := xmlquery.FindOne(node, "./*[local-name() = '"+key+"']/text()")
		if keyNode != nil {
			path += "[" + key + "=" + strings.TrimSpace(keyNode.Data) + "]"
		}
	}

	return path
}

func subscriptionKeySet(path string) map[string]struct{} {
	keys := subscriptionKeys(path)
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	return keySet
}

func subscriptionKeys(path string) []string {
	pathWithoutKeys := stripSubscriptionPredicates(path)
	for _, keyPath := range subscriptionKeyPathCandidates(pathWithoutKeys) {
		if strings.Contains(keyPath, "sonic") {
			if keys := netconf_codegen.SonicMap[keyPath]; len(keys) > 0 {
				return keys
			}
			continue
		}
		if keys := netconf_codegen.CommonMap[keyPath]; len(keys) > 0 {
			return keys
		}
	}
	return nil
}

func subscriptionKeyPathCandidates(path string) []string {
	candidates := []string{path}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" || strings.Contains(parts[0], ":") {
		return candidates
	}

	prefixedRoot := parts[0] + ":" + parts[0]
	candidates = append(candidates, "/"+prefixedRoot)
	if len(parts) > 1 {
		candidates[len(candidates)-1] += "/" + strings.Join(parts[1:], "/")
	}
	return candidates
}

func stripSubscriptionPredicates(path string) string {
	var b strings.Builder
	inPredicate := false
	for _, r := range path {
		switch r {
		case '[':
			inPredicate = true
		case ']':
			inPredicate = false
		default:
			if !inPredicate {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func hasDirectText(node *xmlquery.Node) bool {
	textNode := xmlquery.FindOne(node, "text()")
	return textNode != nil && strings.TrimSpace(textNode.Data) != ""
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	var deduped []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		deduped = append(deduped, value)
	}
	return deduped
}

func isSimpleSubscriptionXPath(selectAttr string) bool {
	if !strings.HasPrefix(selectAttr, "/") {
		return false
	}
	return !strings.ContainsAny(selectAttr, "|()")
}

func ParseGetRequest(node *xmlquery.Node, appendAll bool) ([]GetRequest, error) {
	// TODO: check source tag for config source, for now assume always running config
	// TODO: request path creation assumes parent -> one child structure in filter tag, validation required

	glog.Info("Starting parse Get request")

	// Start with filter node
	filterNode := xmlquery.FindOne(node, "//filter")

	if filterNode == nil {
		return []GetRequest{}, errors.New("Need filter element, can't return all configuration for now")
	}

	queryPaths := []GetRequest{}

	// create base url i.e modelname
	containers := xmlquery.Find(filterNode, "./*") // get child node

	for _, modelContainer := range containers {

		glog.Infof("Parsing for model %s started", modelContainer.Data)

		// Single request
		mainPath := "/" + modelContainer.Data + ":" + modelContainer.Data //translib path building
		glog.Infof("Main path updated %s", mainPath)

		// inner containers
		innerContainers := xmlquery.Find(modelContainer, "./*")

		if len(innerContainers) == 0 {
			glog.Info("main container Children are zero, appending")
			queryPaths = append(queryPaths, GetRequest{path: mainPath, filters: []string{}})
			continue
		}

		// Handler inner container
		for _, innerContainer := range xmlquery.Find(modelContainer, "./*") {

			glog.Infof("Inner container %+v", innerContainer)
			if innerContainer == nil {
				queryPaths = append(queryPaths, GetRequest{path: mainPath, filters: []string{}})
				continue
			}

			path := mainPath + "/" + innerContainer.Data
			glog.Infof("Path updated %s", path)

			// Handle each "list" inside innerContainer
			children := xmlquery.Find(innerContainer, "./*")

			glog.Infof("child len %d", len(children))

			if len(children) == 0 {
				glog.Info("inner container Children are zero, appending")
				queryPaths = append(queryPaths, GetRequest{path: path, filters: []string{}})
				continue
			}

			for _, child := range children {

				glog.Info("Child parsing started %v", child)

				innerPath := path + "/" + child.Data
				glog.Infof("Path updated %s", innerPath)

				var keys []string

				if strings.Contains(innerPath, "sonic") {
					keys = netconf_codegen.SonicMap[innerPath]
					glog.Infof("Sonic path detected, using sonic keys %v", keys)
				} else {
					keys = netconf_codegen.CommonMap[innerPath]
					glog.Infof("Common path detected, using common keys %v", keys)
				}

				glog.Infof("Searching for keys and updating path")

				for _, key := range keys {
					keyNode := xmlquery.FindOne(child, "//*[local-name() = '"+key+"']")

					if keyNode != nil {
						glog.Infof("Keynode %s found %+v", key, keyNode)

						dataNode := xmlquery.FindOne(child, "//*[local-name() = '"+key+"']/text()")

						if dataNode != nil {
							innerPath += "[" + key + "=" + fmt.Sprintf("%v", strings.TrimSpace(dataNode.Data)) + "]"
							glog.Infof("Path updated %s", innerPath)
						}

						// else{
						// 	glog.Infof("Key [%s] data is empty, should be used as list filter", key)
						// 	listFilters = append(listFilters, key)
						// }
					}
				}

				listFilters := []string{}

				leafs := xmlquery.Find(child, "./*")

				for _, leaf := range leafs {
					dataNode := xmlquery.FindOne(child, "//*[local-name() = '"+leaf.Data+"']/text()")
					if dataNode == nil {
						glog.Infof("Empty leaf, should be used as list filter", leaf.Data)
						listFilters = append(listFilters, leaf.Data)
					}
				}

				glog.Infof("Filters for list %+s %+v", child.Data, listFilters)

				// if keysFound == 0 {
				// 	glog.Infof("List level request without any keys, returning entire list")
				// }

				queryPaths = append(queryPaths, GetRequest{path: innerPath, filters: listFilters})
			}

			glog.Infof("Parsing for model %s ended", modelContainer.Data)
		}

	}

	glog.Infof("Paths found in get request", queryPaths)

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
