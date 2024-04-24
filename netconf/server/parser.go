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
	"errors"
	"fmt"
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

func ParseGetRequest(node *xmlquery.Node) ([]GetRequest, error) {
	
	// TODO: check source tag for config source, for now assume always running config
	// TODO: request path creation assumes parent -> one child structure in filter tag, validation required

	// Start with filter node
	filterNode := xmlquery.FindOne(node, "//filter")

	if filterNode == nil {
		return []GetRequest{}, errors.New("[Missing data] Need filter element. Complete configuration retrival currently not supported")
	}

	queryPaths := []GetRequest{}

	containers := xmlquery.Find(filterNode, "./*")

	for _, modelContainer := range containers {

		glog.V(0).Infof("Parsing for model %s started", modelContainer.Data)

		// Single request
		mainPath := "/" + modelContainer.Data + ":" + modelContainer.Data //translib path building
		
		glog.V(0).Infof("Main path updated %s", mainPath)

		// inner containers
		innerContainers := xmlquery.Find(modelContainer, "./*")

		if len(innerContainers) == 0 {
			glog.V(0).Info("Main container Children are zero, appending")
			queryPaths = append(queryPaths, GetRequest{path: mainPath, filters: []string{}})
			continue
		}

		// Handler inner container
		for _, innerContainer := range xmlquery.Find(modelContainer, "./*") {

			glog.V(0).Infof("Inner container %+v", innerContainer)
			
			if innerContainer == nil {
				queryPaths = append(queryPaths, GetRequest{path: mainPath, filters: []string{}})
				continue
			}

			path := mainPath + "/" + innerContainer.Data

			glog.V(0).Infof("Current path updated %s", path)

			// Handle each "list" inside innerContainer
			children := xmlquery.Find(innerContainer, "./*")

			glog.V(0).Infof("Children length %d", len(children))

			if len(children) == 0 {
				glog.V(0).Info("Inner container Children are zero, appending")
				queryPaths = append(queryPaths, GetRequest{path: path, filters: []string{}})
				continue
			}

			for _, child := range children {

				glog.V(0).Infof("Children parsing started %v", child)

				innerPath := path + "/" + child.Data

				glog.V(0).Infof("Inner path updated %s", innerPath)

				var keys []string

				if strings.Contains(innerPath, "sonic") {
					keys = netconf_codegen.SonicMap[innerPath]
					glog.V(0).Infof("Sonic path detected, using sonic keys %v", keys)
				} else {
					keys = netconf_codegen.CommonMap[innerPath]
					glog.V(0).Infof("Common path detected, using common keys %v", keys)
				}

				glog.V(0).Infof("Searching for keys and updating path")

				listFilters := []string{}

				for _, key := range keys {
					keyNode := xmlquery.FindOne(child, "//*[local-name() = '"+key+"']")
					
					if keyNode != nil {
						glog.V(0).Infof("Keynode %s found %+v", key, keyNode)

						dataNode := xmlquery.FindOne(child, "//*[local-name() = '"+key+"']/text()")

						if dataNode != nil {
							innerPath += "[" + key + "=" + fmt.Sprintf("%v", strings.TrimSpace(dataNode.Data)) + "]"
							glog.V(0).Infof("Inner path updated %s", innerPath)
						}else{
							glog.V(0).Infof("Key [%s] data is empty, should be used as list filter", key)
							listFilters = append(listFilters, key)
						}
					}
				}

				glog.V(0).Infof("Filters for list %+s [%+v]", child.Data, listFilters)

				// if keysFound == 0 {
				// 	glog.Infof("List level request without any keys, returning entire list")
				// }

				queryPaths = append(queryPaths, GetRequest{path: innerPath, filters: listFilters})
			}

			glog.V(0).Infof("Parsing for model %s ended", modelContainer.Data)
		}

	}

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