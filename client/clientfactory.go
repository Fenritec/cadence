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

package client

import (
	"fmt"
	"time"

	"go.uber.org/yarpc/api/transport"
	"go.uber.org/yarpc/transport/grpc"

	"github.com/uber/cadence/.gen/go/admin/adminserviceclient"
	"github.com/uber/cadence/.gen/go/cadence/workflowserviceclient"
	"github.com/uber/cadence/.gen/go/history/historyserviceclient"
	"github.com/uber/cadence/.gen/go/matching/matchingserviceclient"

	adminv1 "github.com/uber/cadence/.gen/proto/admin/v1"
	apiv1 "github.com/uber/cadence/.gen/proto/api/v1"
	historyv1 "github.com/uber/cadence/.gen/proto/history/v1"
	matchingv1 "github.com/uber/cadence/.gen/proto/matching/v1"

	"github.com/uber/cadence/client/admin"
	"github.com/uber/cadence/client/frontend"
	"github.com/uber/cadence/client/history"
	"github.com/uber/cadence/client/matching"
	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/dynamicconfig"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/membership"
	"github.com/uber/cadence/common/metrics"
	"github.com/uber/cadence/common/service"
)

const (
	frontendCaller = "cadence-frontend-client"
	historyCaller  = "history-service-client"
	matchingCaller = "matching-service-client"
)

type (
	// Factory can be used to create RPC clients for cadence services
	Factory interface {
		NewHistoryClient() (history.Client, error)
		NewMatchingClient(domainIDToName DomainIDToNameFunc) (matching.Client, error)

		NewHistoryClientWithTimeout(timeout time.Duration) (history.Client, error)
		NewMatchingClientWithTimeout(domainIDToName DomainIDToNameFunc, timeout time.Duration, longPollTimeout time.Duration) (matching.Client, error)

		NewAdminClientWithTimeoutAndConfig(config transport.ClientConfig, timeout time.Duration, largeTimeout time.Duration) (admin.Client, error)
		NewFrontendClientWithTimeoutAndConfig(config transport.ClientConfig, timeout time.Duration, longPollTimeout time.Duration) (frontend.Client, error)
	}

	// DomainIDToNameFunc maps a domainID to domain name. Returns error when mapping is not possible.
	DomainIDToNameFunc func(string) (string, error)

	rpcClientFactory struct {
		rpcFactory            common.RPCFactory
		monitor               membership.Monitor
		metricsClient         metrics.Client
		dynConfig             *dynamicconfig.Collection
		numberOfHistoryShards int
		logger                log.Logger
		enableGRPCOutbound    bool
	}
)

// NewRPCClientFactory creates an instance of client factory that knows how to dispatch RPC calls.
func NewRPCClientFactory(
	rpcFactory common.RPCFactory,
	monitor membership.Monitor,
	metricsClient metrics.Client,
	dc *dynamicconfig.Collection,
	numberOfHistoryShards int,
	logger log.Logger,
) Factory {
	enableGRPCOutbound := dc.GetBoolProperty(dynamicconfig.EnableGRPCOutbound, true)()
	return &rpcClientFactory{
		rpcFactory:            rpcFactory,
		monitor:               monitor,
		metricsClient:         metricsClient,
		dynConfig:             dc,
		numberOfHistoryShards: numberOfHistoryShards,
		logger:                logger,
		enableGRPCOutbound:    enableGRPCOutbound,
	}
}

func (cf *rpcClientFactory) NewHistoryClient() (history.Client, error) {
	return cf.NewHistoryClientWithTimeout(history.DefaultTimeout)
}

func (cf *rpcClientFactory) NewMatchingClient(domainIDToName DomainIDToNameFunc) (matching.Client, error) {
	return cf.NewMatchingClientWithTimeout(domainIDToName, matching.DefaultTimeout, matching.DefaultLongPollTimeout)
}

func (cf *rpcClientFactory) createKeyResolver(serviceName string) (func(key string) (string, error), error) {
	resolver, err := cf.monitor.GetResolver(serviceName)
	if err != nil {
		return nil, err
	}

	return func(key string) (string, error) {
		host, err := resolver.Lookup(key)
		if err != nil {
			return "", err
		}
		hostAddress := host.GetAddress()
		if cf.enableGRPCOutbound {
			hostAddress, err = cf.rpcFactory.ReplaceGRPCPort(serviceName, hostAddress)
			if err != nil {
				return "", err
			}
		}
		return hostAddress, nil
	}, nil
}

func (cf *rpcClientFactory) NewHistoryClientWithTimeout(timeout time.Duration) (history.Client, error) {
	keyResolver, err := cf.createKeyResolver(service.History)
	if err != nil {
		return nil, err
	}

	clientProvider := func(clientKey string) (interface{}, error) {
		if cf.enableGRPCOutbound {
			return cf.newHistoryGRPCClient(clientKey)
		}
		return cf.newHistoryThriftClient(clientKey)
	}

	supportedMessageSize := cf.rpcFactory.GetMaxMessageSize()
	maxSizeConfig := cf.dynConfig.GetIntProperty(dynamicconfig.GRPCMaxSizeInByte, supportedMessageSize)
	if maxSizeConfig() > supportedMessageSize {
		return nil, fmt.Errorf(
			"GRPCMaxSizeInByte dynamic config value %v is larger than supported value %v",
			maxSizeConfig(),
			supportedMessageSize,
		)
	}
	client := history.NewClient(
		cf.numberOfHistoryShards,
		maxSizeConfig,
		timeout,
		common.NewClientCache(keyResolver, clientProvider),
		cf.logger,
	)
	if errorRate := cf.dynConfig.GetFloat64Property(dynamicconfig.HistoryErrorInjectionRate, 0)(); errorRate != 0 {
		client = history.NewErrorInjectionClient(client, errorRate, cf.logger)
	}
	if cf.metricsClient != nil {
		client = history.NewMetricClient(client, cf.metricsClient)
	}
	return client, nil
}

func (cf *rpcClientFactory) NewMatchingClientWithTimeout(
	domainIDToName DomainIDToNameFunc,
	timeout time.Duration,
	longPollTimeout time.Duration,
) (matching.Client, error) {
	keyResolver, err := cf.createKeyResolver(service.Matching)
	if err != nil {
		return nil, err
	}

	clientProvider := func(clientKey string) (interface{}, error) {
		if cf.enableGRPCOutbound {
			return cf.newMatchingGRPCClient(clientKey)
		}
		return cf.newMatchingThriftClient(clientKey)
	}

	clientFetcher := func() ([]matching.Client, error) {
		resolver, err := cf.monitor.GetResolver(service.Matching)
		if err != nil {
			return nil, err
		}

		var result []matching.Client
		for _, host := range resolver.Members() {
			hostAddress := host.GetAddress()
			if cf.enableGRPCOutbound {
				hostAddress, err = cf.rpcFactory.ReplaceGRPCPort(service.Matching, hostAddress)
				if err != nil {
					return nil, err
				}
			}
			client, err := clientProvider(hostAddress)
			if err != nil {
				return nil, err
			}
			result = append(result, client.(matching.Client))
		}
		return result, nil
	}

	client := matching.NewClient(
		timeout,
		longPollTimeout,
		common.NewClientCache(keyResolver, clientProvider),
		matching.NewLoadBalancer(domainIDToName, cf.dynConfig),
		clientFetcher,
	)
	if errorRate := cf.dynConfig.GetFloat64Property(dynamicconfig.MatchingErrorInjectionRate, 0)(); errorRate != 0 {
		client = matching.NewErrorInjectionClient(client, errorRate, cf.logger)
	}
	if cf.metricsClient != nil {
		client = matching.NewMetricClient(client, cf.metricsClient)
	}
	return client, nil

}

func (cf *rpcClientFactory) NewAdminClientWithTimeoutAndConfig(
	config transport.ClientConfig,
	timeout time.Duration,
	largeTimeout time.Duration,
) (admin.Client, error) {
	var client admin.Client
	if isGRPCOutbound(config) {
		client = admin.NewGRPCClient(adminv1.NewAdminAPIYARPCClient(config))
	} else {
		client = admin.NewThriftClient(adminserviceclient.New(config))
	}

	client = admin.NewClient(timeout, largeTimeout, client)
	if errorRate := cf.dynConfig.GetFloat64Property(dynamicconfig.AdminErrorInjectionRate, 0)(); errorRate != 0 {
		client = admin.NewErrorInjectionClient(client, errorRate, cf.logger)
	}
	if cf.metricsClient != nil {
		client = admin.NewMetricClient(client, cf.metricsClient)
	}
	return client, nil
}

func (cf *rpcClientFactory) NewFrontendClientWithTimeoutAndConfig(
	config transport.ClientConfig,
	timeout time.Duration,
	longPollTimeout time.Duration,
) (frontend.Client, error) {
	var client frontend.Client
	if isGRPCOutbound(config) {
		client = frontend.NewGRPCClient(
			apiv1.NewDomainAPIYARPCClient(config),
			apiv1.NewWorkflowAPIYARPCClient(config),
			apiv1.NewWorkerAPIYARPCClient(config),
			apiv1.NewVisibilityAPIYARPCClient(config),
		)
	} else {
		client = frontend.NewThriftClient(workflowserviceclient.New(config))
	}

	client = frontend.NewClient(timeout, longPollTimeout, client)
	if errorRate := cf.dynConfig.GetFloat64Property(dynamicconfig.FrontendErrorInjectionRate, 0)(); errorRate != 0 {
		client = frontend.NewErrorInjectionClient(client, errorRate, cf.logger)
	}
	if cf.metricsClient != nil {
		client = frontend.NewMetricClient(client, cf.metricsClient)
	}
	return client, nil
}

func (cf *rpcClientFactory) newHistoryThriftClient(hostAddress string) (history.Client, error) {
	dispatcher, err := cf.rpcFactory.CreateDispatcherForOutbound(historyCaller, service.History, hostAddress)
	if err != nil {
		return nil, err
	}
	return history.NewThriftClient(historyserviceclient.New(dispatcher.ClientConfig(service.History))), nil
}

func (cf *rpcClientFactory) newMatchingThriftClient(hostAddress string) (matching.Client, error) {
	dispatcher, err := cf.rpcFactory.CreateDispatcherForOutbound(matchingCaller, service.Matching, hostAddress)
	if err != nil {
		return nil, err
	}
	return matching.NewThriftClient(matchingserviceclient.New(dispatcher.ClientConfig(service.Matching))), nil
}

func (cf *rpcClientFactory) newFrontendThriftClient(hostAddress string) (frontend.Client, error) {
	dispatcher, err := cf.rpcFactory.CreateDispatcherForOutbound(frontendCaller, service.Frontend, hostAddress)
	if err != nil {
		return nil, err
	}
	return frontend.NewThriftClient(workflowserviceclient.New(dispatcher.ClientConfig(service.Frontend))), nil
}

func (cf *rpcClientFactory) newHistoryGRPCClient(hostAddress string) (history.Client, error) {
	dispatcher, err := cf.rpcFactory.CreateGRPCDispatcherForOutbound(historyCaller, service.History, hostAddress)
	if err != nil {
		return nil, err
	}
	return history.NewGRPCClient(historyv1.NewHistoryAPIYARPCClient(dispatcher.ClientConfig(service.History))), nil
}

func (cf *rpcClientFactory) newMatchingGRPCClient(hostAddress string) (matching.Client, error) {
	dispatcher, err := cf.rpcFactory.CreateGRPCDispatcherForOutbound(matchingCaller, service.Matching, hostAddress)
	if err != nil {
		return nil, err
	}
	return matching.NewGRPCClient(matchingv1.NewMatchingAPIYARPCClient(dispatcher.ClientConfig(service.Matching))), nil
}

func (cf *rpcClientFactory) newFrontendGRPCClient(hostAddress string) (frontend.Client, error) {
	dispatcher, err := cf.rpcFactory.CreateGRPCDispatcherForOutbound(frontendCaller, service.Frontend, hostAddress)
	if err != nil {
		return nil, err
	}
	config := dispatcher.ClientConfig(service.Frontend)
	return frontend.NewGRPCClient(
		apiv1.NewDomainAPIYARPCClient(config),
		apiv1.NewWorkflowAPIYARPCClient(config),
		apiv1.NewWorkerAPIYARPCClient(config),
		apiv1.NewVisibilityAPIYARPCClient(config),
	), nil
}

func isGRPCOutbound(config transport.ClientConfig) bool {
	namer, ok := config.GetUnaryOutbound().(transport.Namer)
	if !ok {
		// This should not happen, unless yarpc older than v1.43.0 is used
		panic("Outbound does not implement transport.Namer")
	}
	return namer.TransportName() == grpc.TransportName
}
