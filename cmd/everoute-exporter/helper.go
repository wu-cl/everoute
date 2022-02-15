/*
Copyright 2021 The Everoute Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"

	log "github.com/Sirupsen/logrus"
	ovsdb "github.com/contiv/libovsdb"
)

const emptyUUID = "00000000-0000-0000-0000-000000000000"

func ovsdbTransact(client *ovsdb.OvsdbClient, database string, operation ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	results, err := client.Transact(database, operation...)
	for item, result := range results {
		if result.Error != "" {
			return results, fmt.Errorf("operator %v: %s, details: %s", operation[item], result.Error, result.Details)
		}
	}

	return results, err
}

func configSflowForOVS(port int) error {
	client, err := ovsdb.ConnectUnix(ovsdb.DEFAULT_SOCK)
	if err != nil {
		return fmt.Errorf("connect to ovsdb: %s", err)
	}

	createSflowOperation := ovsdb.Operation{
		UUIDName: "dummy",
		Op:       "insert",
		Table:    "sFlow",
		Row: map[string]interface{}{
			"header":   128,
			"polling":  10,
			"targets":  fmt.Sprintf("127.0.0.1:%d", port),
			"sampling": 64,
		},
	}

	configBridgeOperation := ovsdb.Operation{
		Op:    "update",
		Table: "Bridge",
		Row:   map[string]interface{}{"sflow": ovsdb.UUID{GoUuid: "dummy"}},
		Where: []interface{}{[]interface{}{"_uuid", "excludes", ovsdb.UUID{GoUuid: emptyUUID}}},
	}

	_, err = ovsdbTransact(client, "Open_vSwitch", createSflowOperation, configBridgeOperation)
	if err != nil {
		return fmt.Errorf("set bridges sflow: %s", err)
	}

	return nil
}

func cleanOVSConfig() error {
	client, err := ovsdb.ConnectUnix(ovsdb.DEFAULT_SOCK)
	if err != nil {
		return fmt.Errorf("connect to ovsdb: %s", err)
	}

	configBridgeOperation := ovsdb.Operation{
		Op:    "update",
		Table: "Bridge",
		Row:   map[string]interface{}{"sflow": ovsdb.OvsSet{GoSet: []interface{}{}}},
		Where: []interface{}{[]interface{}{"_uuid", "excludes", ovsdb.UUID{GoUuid: emptyUUID}}},
	}

	_, err = ovsdbTransact(client, "Open_vSwitch", configBridgeOperation)
	if err != nil {
		return fmt.Errorf("clean bridges sflow: %s", err)
	}

	return nil
}

func mustCleanOVSConfig() {
	log.Printf("Cleaning OVS configuration")
	err := cleanOVSConfig()
	if err != nil {
		panic(err)
	}
}
