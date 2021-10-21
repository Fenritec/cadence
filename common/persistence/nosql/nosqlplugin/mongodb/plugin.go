// Copyright (c) 2020 Uber Technologies, Inc.
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

package mongodb

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/uber/cadence/common/config"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/persistence/nosql"
	"github.com/uber/cadence/common/persistence/nosql/nosqlplugin"
)

const (
	// PluginName is the name of the plugin
	PluginName            = "mongodb"
	defaultSessionTimeout = 10 * time.Second
)

type plugin struct{}

var _ nosqlplugin.Plugin = (*plugin)(nil)

func init() {
	nosql.RegisterPlugin(PluginName, &plugin{})
}

// CreateDB initialize the db object
func (p *plugin) CreateDB(cfg *config.NoSQL, logger log.Logger) (nosqlplugin.DB, error) {
	return p.doCreateDB(cfg, logger)
}

// CreateAdminDB initialize the AdminDB object
func (p *plugin) CreateAdminDB(cfg *config.NoSQL, logger log.Logger) (nosqlplugin.AdminDB, error) {
	return p.doCreateDB(cfg, logger)
}

func (p *plugin) doCreateDB(cfg *config.NoSQL, logger log.Logger) (*mdb, error) {
	uri := fmt.Sprintf("mongodb://%v:%v@%v:%v/", cfg.User, cfg.Password, cfg.Hosts, cfg.Port)
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
	return &mdb{
		client: client,
		logger: logger,
	}, err
}


