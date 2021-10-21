// Copyright (c) 2017 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package tests

import (
	"log"
	"testing"

	"github.com/uber/cadence/common/config"
	"github.com/uber/cadence/common/persistence/nosql"
	"github.com/uber/cadence/common/persistence/nosql/nosqlplugin/mongodb"
)

func getTestConfig() *config.NoSQL {
	return &config.NoSQL{
		PluginName: mongodb.PluginName,
		User:       "root",
		Password:   "cadence",
		Hosts:      "localhost",
		Port:       27017,
	}
}

// TestConnection will test connecting to mongo is successful
func TestConnection(t *testing.T) {
	_, err := nosql.NewNoSQLAdminDB(getTestConfig(), nil)
	if err != nil {
		log.Fatal("fail to connect to mongo")
	}
}

func TestMongoDBHistoryPersistence(t *testing.T) {
	// s := new(persistencetests.HistoryV2PersistenceSuite)
	// s.TestBase = public.NewTestBaseWithMongoDB(&persistencetests.TestBaseOptions{})
	// s.TestBase.Setup()
	//suite.Run(t, s)
}

func TestMongoDBMatchingPersistence(t *testing.T) {
	//s := new(persistencetests.MatchingPersistenceSuite)
	//s.TestBase = public.NewTestBaseWithMongoDB(&persistencetests.TestBaseOptions{})
	//s.TestBase.Setup()
	//suite.Run(t, s)
}

func TestMongoDBDomainPersistence(t *testing.T) {
	//s := new(persistencetests.MetadataPersistenceSuiteV2)
	//s.TestBase = public.NewTestBaseWithMongoDB(&persistencetests.TestBaseOptions{})
	//s.TestBase.Setup()
	//suite.Run(t, s)
}

func TestQueuePersistence(t *testing.T) {
	//s := new(persistencetests.QueuePersistenceSuite)
	//s.TestBase = public.NewTestBaseWithMongoDB(&persistencetests.TestBaseOptions{})
	//s.TestBase.Setup()
	//suite.Run(t, s)
}
