// Copyright (c) 2017-2020 Uber Technologies, Inc.
// Portions of the Software are attributed to Copyright (c) 2020 Temporal Technologies Inc.
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

package persistencetests

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/pborman/uuid"
	"github.com/stretchr/testify/suite"
	"github.com/uber-go/tally"

	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/backoff"
	"github.com/uber/cadence/common/cluster"
	"github.com/uber/cadence/common/config"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/log/loggerimpl"
	"github.com/uber/cadence/common/log/tag"
	"github.com/uber/cadence/common/metrics"
	"github.com/uber/cadence/common/persistence"
	p "github.com/uber/cadence/common/persistence"
	"github.com/uber/cadence/common/persistence/client"
	"github.com/uber/cadence/common/persistence/nosql"
	"github.com/uber/cadence/common/persistence/persistence-tests/testcluster"
	"github.com/uber/cadence/common/persistence/sql"
	"github.com/uber/cadence/common/service"
	"github.com/uber/cadence/common/types"
)

type (
	// TransferTaskIDGenerator generates IDs for transfer tasks written by helper methods
	TransferTaskIDGenerator interface {
		GenerateTransferTaskID() (int64, error)
	}

	// TestBaseOptions options to configure workflow test base.
	TestBaseOptions struct {
		DBPluginName    string
		DBName          string
		DBUsername      string
		DBPassword      string
		DBHost          string
		DBPort          int              `yaml:"-"`
		StoreType       string           `yaml:"-"`
		SchemaDir       string           `yaml:"-"`
		ClusterMetadata cluster.Metadata `yaml:"-"`
		ProtoVersion    int              `yaml:"-"`
	}

	// TestBase wraps the base setup needed to create workflows over persistence layer.
	TestBase struct {
		suite.Suite
		ShardMgr                  p.ShardManager
		ExecutionMgrFactory       client.Factory
		ExecutionManager          p.ExecutionManager
		TaskMgr                   p.TaskManager
		HistoryV2Mgr              p.HistoryManager
		DomainManager             p.DomainManager
		DomainReplicationQueueMgr p.QueueManager
		ShardInfo                 *p.ShardInfo
		TaskIDGenerator           TransferTaskIDGenerator
		ClusterMetadata           cluster.Metadata
		DefaultTestCluster        testcluster.PersistenceTestCluster
		VisibilityTestCluster     testcluster.PersistenceTestCluster
		Logger                    log.Logger
		PayloadSerializer         p.PayloadSerializer
		ConfigStoreManager        p.ConfigStoreManager
	}

	// TestBaseParams defines the input of TestBase
	TestBaseParams struct {
		DefaultTestCluster    testcluster.PersistenceTestCluster
		VisibilityTestCluster testcluster.PersistenceTestCluster
		ClusterMetadata       cluster.Metadata
	}

	// TestTransferTaskIDGenerator helper
	TestTransferTaskIDGenerator struct {
		seqNum int64
	}
)

const (
	defaultScheduleToStartTimeout = 111
)

// NewTestBaseFromParams returns a customized test base from given input
func NewTestBaseFromParams(params TestBaseParams) TestBase {
	logger, err := loggerimpl.NewDevelopment()
	if err != nil {
		panic(err)
	}
	return TestBase{
		DefaultTestCluster:    params.DefaultTestCluster,
		VisibilityTestCluster: params.VisibilityTestCluster,
		ClusterMetadata:       params.ClusterMetadata,
		PayloadSerializer:     p.NewPayloadSerializer(),
		Logger:                logger,
	}
}

// NewTestBaseWithNoSQL returns a persistence test base backed by nosql datastore
func NewTestBaseWithNoSQL(options *TestBaseOptions) TestBase {
	if options.DBName == "" {
		options.DBName = "test_" + GenerateRandomDBName(10)
	}
	testCluster := nosql.NewTestCluster(options.DBPluginName, options.DBName, options.DBUsername, options.DBPassword, options.DBHost, options.DBPort, options.ProtoVersion, "")
	metadata := options.ClusterMetadata
	if metadata == nil {
		metadata = cluster.GetTestClusterMetadata(false, false)
	}
	params := TestBaseParams{
		DefaultTestCluster:    testCluster,
		VisibilityTestCluster: testCluster,
		ClusterMetadata:       metadata,
	}
	return NewTestBaseFromParams(params)
}

// NewTestBaseWithSQL returns a new persistence test base backed by SQL
func NewTestBaseWithSQL(options *TestBaseOptions) TestBase {
	if options.DBName == "" {
		options.DBName = "test_" + GenerateRandomDBName(10)
	}
	testCluster := sql.NewTestCluster(options.DBPluginName, options.DBName, options.DBUsername, options.DBPassword, options.DBHost, options.DBPort, options.SchemaDir)
	metadata := options.ClusterMetadata
	if metadata == nil {
		metadata = cluster.GetTestClusterMetadata(false, false)
	}
	params := TestBaseParams{
		DefaultTestCluster:    testCluster,
		VisibilityTestCluster: testCluster,
		ClusterMetadata:       metadata,
	}
	return NewTestBaseFromParams(params)
}

// Config returns the persistence configuration for this test
func (s *TestBase) Config() config.Persistence {
	cfg := s.DefaultTestCluster.Config()
	if s.DefaultTestCluster == s.VisibilityTestCluster {
		return cfg
	}
	vCfg := s.VisibilityTestCluster.Config()
	cfg.VisibilityStore = "visibility_ " + vCfg.VisibilityStore
	cfg.DataStores[cfg.VisibilityStore] = vCfg.DataStores[vCfg.VisibilityStore]
	return cfg
}

// Setup sets up the test base, must be called as part of SetupSuite
func (s *TestBase) Setup() {
	var err error
	shardID := 10
	clusterName := s.ClusterMetadata.GetCurrentClusterName()

	s.DefaultTestCluster.SetupTestDatabase()

	cfg := s.DefaultTestCluster.Config()
	scope := tally.NewTestScope(service.History, make(map[string]string))
	metricsClient := metrics.NewClient(scope, service.GetMetricsServiceIdx(service.History, s.Logger))
	factory := client.NewFactory(&cfg, nil, clusterName, metricsClient, s.Logger)

	s.TaskMgr, err = factory.NewTaskManager()
	s.fatalOnError("NewTaskManager", err)

	s.DomainManager, err = factory.NewDomainManager()
	s.fatalOnError("NewDomainManager", err)

	s.HistoryV2Mgr, err = factory.NewHistoryManager()
	s.fatalOnError("NewHistoryManager", err)

	s.ShardMgr, err = factory.NewShardManager()
	s.fatalOnError("NewShardManager", err)

	if cfg.DefaultStoreType() == config.StoreTypeCassandra {
		s.ConfigStoreManager, err = factory.NewConfigStoreManager()
		s.fatalOnError("NewConfigStoreManager", err)
	}

	s.ExecutionMgrFactory = factory
	s.ExecutionManager, err = factory.NewExecutionManager(shardID)
	s.fatalOnError("NewExecutionManager", err)

	domainFilter := &types.DomainFilter{
		DomainIDs:    []string{},
		ReverseMatch: true,
	}
	transferPQSMap := map[string][]*types.ProcessingQueueState{
		s.ClusterMetadata.GetCurrentClusterName(): {
			&types.ProcessingQueueState{
				Level:        common.Int32Ptr(0),
				AckLevel:     common.Int64Ptr(0),
				MaxLevel:     common.Int64Ptr(0),
				DomainFilter: domainFilter,
			},
		},
	}
	transferPQS := types.ProcessingQueueStates{StatesByCluster: transferPQSMap}
	timerPQSMap := map[string][]*types.ProcessingQueueState{
		s.ClusterMetadata.GetCurrentClusterName(): {
			&types.ProcessingQueueState{
				Level:        common.Int32Ptr(0),
				AckLevel:     common.Int64Ptr(time.Now().UnixNano()),
				MaxLevel:     common.Int64Ptr(time.Now().UnixNano()),
				DomainFilter: domainFilter,
			},
		},
	}
	timerPQS := types.ProcessingQueueStates{StatesByCluster: timerPQSMap}

	s.ShardInfo = &p.ShardInfo{
		ShardID:                       shardID,
		RangeID:                       0,
		TransferAckLevel:              0,
		ReplicationAckLevel:           0,
		TimerAckLevel:                 time.Time{},
		ClusterTimerAckLevel:          map[string]time.Time{clusterName: time.Time{}},
		ClusterTransferAckLevel:       map[string]int64{clusterName: 0},
		TransferProcessingQueueStates: &transferPQS,
		TimerProcessingQueueStates:    &timerPQS,
	}

	s.TaskIDGenerator = &TestTransferTaskIDGenerator{}
	err = s.ShardMgr.CreateShard(context.Background(), &p.CreateShardRequest{ShardInfo: s.ShardInfo})
	s.fatalOnError("CreateShard", err)

	queue, err := factory.NewDomainReplicationQueueManager()
	s.fatalOnError("Create DomainReplicationQueue", err)
	s.DomainReplicationQueueMgr = queue
}

func (s *TestBase) fatalOnError(msg string, err error) {
	if err != nil {
		s.Logger.Fatal(msg, tag.Error(err))
	}
}

// CreateShard is a utility method to create the shard using persistence layer
func (s *TestBase) CreateShard(ctx context.Context, shardID int, owner string, rangeID int64) error {
	info := &p.ShardInfo{
		ShardID: shardID,
		Owner:   owner,
		RangeID: rangeID,
	}

	return s.ShardMgr.CreateShard(ctx, &p.CreateShardRequest{
		ShardInfo: info,
	})
}

// GetShard is a utility method to get the shard using persistence layer
func (s *TestBase) GetShard(ctx context.Context, shardID int) (*p.ShardInfo, error) {
	response, err := s.ShardMgr.GetShard(ctx, &p.GetShardRequest{
		ShardID: shardID,
	})

	if err != nil {
		return nil, err
	}

	return response.ShardInfo, nil
}

// UpdateShard is a utility method to update the shard using persistence layer
func (s *TestBase) UpdateShard(ctx context.Context, updatedInfo *p.ShardInfo, previousRangeID int64) error {
	return s.ShardMgr.UpdateShard(ctx, &p.UpdateShardRequest{
		ShardInfo:       updatedInfo,
		PreviousRangeID: previousRangeID,
	})
}

// CreateWorkflowExecutionWithBranchToken test util function
func (s *TestBase) CreateWorkflowExecutionWithBranchToken(
	ctx context.Context,
	domainID string,
	workflowExecution types.WorkflowExecution,
	taskList string,
	wType string,
	wTimeout int32,
	decisionTimeout int32,
	executionContext []byte,
	nextEventID int64,
	lastProcessedEventID int64,
	decisionScheduleID int64,
	branchToken []byte,
	timerTasks []p.Task,
) (*p.CreateWorkflowExecutionResponse, error) {

	now := time.Now()
	versionHistory := p.NewVersionHistory(branchToken, []*p.VersionHistoryItem{
		{decisionScheduleID, common.EmptyVersion},
	})
	versionHistories := p.NewVersionHistories(versionHistory)
	response, err := s.ExecutionManager.CreateWorkflowExecution(ctx, &p.CreateWorkflowExecutionRequest{
		NewWorkflowSnapshot: p.WorkflowSnapshot{
			ExecutionInfo: &p.WorkflowExecutionInfo{
				CreateRequestID:             uuid.New(),
				DomainID:                    domainID,
				WorkflowID:                  workflowExecution.GetWorkflowID(),
				RunID:                       workflowExecution.GetRunID(),
				TaskList:                    taskList,
				WorkflowTypeName:            wType,
				WorkflowTimeout:             wTimeout,
				DecisionStartToCloseTimeout: decisionTimeout,
				ExecutionContext:            executionContext,
				State:                       p.WorkflowStateRunning,
				CloseStatus:                 p.WorkflowCloseStatusNone,
				LastFirstEventID:            common.FirstEventID,
				NextEventID:                 nextEventID,
				LastProcessedEvent:          lastProcessedEventID,
				LastUpdatedTimestamp:        now,
				StartTimestamp:              now,
				DecisionScheduleID:          decisionScheduleID,
				DecisionStartedID:           common.EmptyEventID,
				DecisionTimeout:             1,
				BranchToken:                 branchToken,
			},
			ExecutionStats: &p.ExecutionStats{},
			TransferTasks: []p.Task{
				&p.DecisionTask{
					TaskID:              s.GetNextSequenceNumber(),
					DomainID:            domainID,
					TaskList:            taskList,
					ScheduleID:          decisionScheduleID,
					VisibilityTimestamp: time.Now(),
				},
			},
			TimerTasks:       timerTasks,
			Checksum:         testWorkflowChecksum,
			VersionHistories: versionHistories,
		},
		RangeID: s.ShardInfo.RangeID,
	})

	return response, err
}

// CreateWorkflowExecution is a utility method to create workflow executions
func (s *TestBase) CreateWorkflowExecution(
	ctx context.Context,
	domainID string,
	workflowExecution types.WorkflowExecution,
	taskList string,
	wType string,
	wTimeout int32,
	decisionTimeout int32,
	executionContext []byte,
	nextEventID int64,
	lastProcessedEventID int64,
	decisionScheduleID int64,
	timerTasks []p.Task,
) (*p.CreateWorkflowExecutionResponse, error) {

	return s.CreateWorkflowExecutionWithBranchToken(ctx, domainID, workflowExecution, taskList, wType, wTimeout, decisionTimeout,
		executionContext, nextEventID, lastProcessedEventID, decisionScheduleID, nil, timerTasks)
}

// CreateChildWorkflowExecution is a utility method to create child workflow executions
func (s *TestBase) CreateChildWorkflowExecution(ctx context.Context, domainID string, workflowExecution types.WorkflowExecution,
	parentDomainID string, parentExecution types.WorkflowExecution, initiatedID int64, taskList, wType string,
	wTimeout int32, decisionTimeout int32, executionContext []byte, nextEventID int64, lastProcessedEventID int64,
	decisionScheduleID int64, timerTasks []p.Task) (*p.CreateWorkflowExecutionResponse, error) {
	now := time.Now()
	versionHistory := p.NewVersionHistory([]byte{}, []*p.VersionHistoryItem{
		{decisionScheduleID, common.EmptyVersion},
	})
	versionHistories := p.NewVersionHistories(versionHistory)
	response, err := s.ExecutionManager.CreateWorkflowExecution(ctx, &p.CreateWorkflowExecutionRequest{
		NewWorkflowSnapshot: p.WorkflowSnapshot{
			ExecutionInfo: &p.WorkflowExecutionInfo{
				CreateRequestID:             uuid.New(),
				DomainID:                    domainID,
				WorkflowID:                  workflowExecution.GetWorkflowID(),
				RunID:                       workflowExecution.GetRunID(),
				ParentDomainID:              parentDomainID,
				ParentWorkflowID:            parentExecution.GetWorkflowID(),
				ParentRunID:                 parentExecution.GetRunID(),
				InitiatedID:                 initiatedID,
				TaskList:                    taskList,
				WorkflowTypeName:            wType,
				WorkflowTimeout:             wTimeout,
				DecisionStartToCloseTimeout: decisionTimeout,
				ExecutionContext:            executionContext,
				State:                       p.WorkflowStateCreated,
				CloseStatus:                 p.WorkflowCloseStatusNone,
				LastFirstEventID:            common.FirstEventID,
				NextEventID:                 nextEventID,
				LastProcessedEvent:          lastProcessedEventID,
				LastUpdatedTimestamp:        now,
				StartTimestamp:              now,
				DecisionScheduleID:          decisionScheduleID,
				DecisionStartedID:           common.EmptyEventID,
				DecisionTimeout:             1,
			},
			ExecutionStats: &p.ExecutionStats{},
			TransferTasks: []p.Task{
				&p.DecisionTask{
					TaskID:     s.GetNextSequenceNumber(),
					DomainID:   domainID,
					TaskList:   taskList,
					ScheduleID: decisionScheduleID,
				},
			},
			TimerTasks:       timerTasks,
			VersionHistories: versionHistories,
		},
		RangeID: s.ShardInfo.RangeID,
	})

	return response, err
}

// GetWorkflowExecutionInfoWithStats is a utility method to retrieve execution info with size stats
func (s *TestBase) GetWorkflowExecutionInfoWithStats(ctx context.Context, domainID string, workflowExecution types.WorkflowExecution) (
	*p.MutableStateStats, *p.WorkflowMutableState, error) {
	response, err := s.ExecutionManager.GetWorkflowExecution(ctx, &p.GetWorkflowExecutionRequest{
		DomainID:  domainID,
		Execution: workflowExecution,
	})
	if err != nil {
		return nil, nil, err
	}

	return response.MutableStateStats, response.State, nil
}

// GetWorkflowExecutionInfo is a utility method to retrieve execution info
func (s *TestBase) GetWorkflowExecutionInfo(ctx context.Context, domainID string, workflowExecution types.WorkflowExecution) (
	*p.WorkflowMutableState, error) {
	response, err := s.ExecutionManager.GetWorkflowExecution(ctx, &p.GetWorkflowExecutionRequest{
		DomainID:  domainID,
		Execution: workflowExecution,
	})
	if err != nil {
		return nil, err
	}
	return response.State, nil
}

// GetCurrentWorkflowRunID returns the workflow run ID for the given params
func (s *TestBase) GetCurrentWorkflowRunID(ctx context.Context, domainID, workflowID string) (string, error) {
	response, err := s.ExecutionManager.GetCurrentExecution(ctx, &p.GetCurrentExecutionRequest{
		DomainID:   domainID,
		WorkflowID: workflowID,
	})

	if err != nil {
		return "", err
	}

	return response.RunID, nil
}

// ContinueAsNewExecution is a utility method to create workflow executions
func (s *TestBase) ContinueAsNewExecution(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	condition int64,
	newExecution types.WorkflowExecution,
	nextEventID, decisionScheduleID int64,
	prevResetPoints *types.ResetPoints,
) error {

	now := time.Now()
	newdecisionTask := &p.DecisionTask{
		TaskID:     s.GetNextSequenceNumber(),
		DomainID:   updatedInfo.DomainID,
		TaskList:   updatedInfo.TaskList,
		ScheduleID: int64(decisionScheduleID),
	}
	versionHistory := p.NewVersionHistory([]byte{}, []*p.VersionHistoryItem{
		{decisionScheduleID, common.EmptyVersion},
	})
	versionHistories := p.NewVersionHistories(versionHistory)

	req := &p.UpdateWorkflowExecutionRequest{
		UpdateWorkflowMutation: p.WorkflowMutation{
			ExecutionInfo:       updatedInfo,
			ExecutionStats:      updatedStats,
			TransferTasks:       []p.Task{newdecisionTask},
			TimerTasks:          nil,
			Condition:           condition,
			UpsertActivityInfos: nil,
			DeleteActivityInfos: nil,
			UpsertTimerInfos:    nil,
			DeleteTimerInfos:    nil,
			VersionHistories:    versionHistories,
		},
		NewWorkflowSnapshot: &p.WorkflowSnapshot{
			ExecutionInfo: &p.WorkflowExecutionInfo{
				CreateRequestID:             uuid.New(),
				DomainID:                    updatedInfo.DomainID,
				WorkflowID:                  newExecution.GetWorkflowID(),
				RunID:                       newExecution.GetRunID(),
				TaskList:                    updatedInfo.TaskList,
				WorkflowTypeName:            updatedInfo.WorkflowTypeName,
				WorkflowTimeout:             updatedInfo.WorkflowTimeout,
				DecisionStartToCloseTimeout: updatedInfo.DecisionStartToCloseTimeout,
				ExecutionContext:            nil,
				State:                       updatedInfo.State,
				CloseStatus:                 updatedInfo.CloseStatus,
				LastFirstEventID:            common.FirstEventID,
				NextEventID:                 nextEventID,
				LastProcessedEvent:          common.EmptyEventID,
				LastUpdatedTimestamp:        now,
				StartTimestamp:              now,
				DecisionScheduleID:          decisionScheduleID,
				DecisionStartedID:           common.EmptyEventID,
				DecisionTimeout:             1,
				AutoResetPoints:             prevResetPoints,
			},
			ExecutionStats:   updatedStats,
			TransferTasks:    nil,
			TimerTasks:       nil,
			VersionHistories: versionHistories,
		},
		RangeID:  s.ShardInfo.RangeID,
		Encoding: pickRandomEncoding(),
	}
	req.UpdateWorkflowMutation.ExecutionInfo.State = p.WorkflowStateCompleted
	req.UpdateWorkflowMutation.ExecutionInfo.CloseStatus = p.WorkflowCloseStatusContinuedAsNew
	_, err := s.ExecutionManager.UpdateWorkflowExecution(ctx, req)
	return err
}

// UpdateWorkflowExecution is a utility method to update workflow execution
func (s *TestBase) UpdateWorkflowExecution(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	decisionScheduleIDs []int64,
	activityScheduleIDs []int64,
	condition int64,
	timerTasks []p.Task,
	upsertActivityInfos []*p.ActivityInfo,
	deleteActivityInfos []int64,
	upsertTimerInfos []*p.TimerInfo,
	deleteTimerInfos []string,
) error {
	return s.UpdateWorkflowExecutionWithRangeID(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		decisionScheduleIDs,
		activityScheduleIDs,
		s.ShardInfo.RangeID,
		condition,
		timerTasks,
		upsertActivityInfos,
		deleteActivityInfos,
		upsertTimerInfos,
		deleteTimerInfos,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

// UpdateWorkflowExecutionAndFinish is a utility method to update workflow execution
func (s *TestBase) UpdateWorkflowExecutionAndFinish(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	condition int64,
	versionHistories *p.VersionHistories,
) error {
	transferTasks := []p.Task{}
	transferTasks = append(transferTasks, &p.CloseExecutionTask{TaskID: s.GetNextSequenceNumber()})
	_, err := s.ExecutionManager.UpdateWorkflowExecution(ctx, &p.UpdateWorkflowExecutionRequest{
		RangeID: s.ShardInfo.RangeID,
		UpdateWorkflowMutation: p.WorkflowMutation{
			ExecutionInfo:       updatedInfo,
			ExecutionStats:      updatedStats,
			TransferTasks:       transferTasks,
			TimerTasks:          nil,
			Condition:           condition,
			UpsertActivityInfos: nil,
			DeleteActivityInfos: nil,
			UpsertTimerInfos:    nil,
			DeleteTimerInfos:    nil,
			VersionHistories:    versionHistories,
		},
		Encoding: pickRandomEncoding(),
	})
	return err
}

// UpsertChildExecutionsState is a utility method to update mutable state of workflow execution
func (s *TestBase) UpsertChildExecutionsState(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	condition int64,
	upsertChildInfos []*p.ChildExecutionInfo,
) error {

	return s.UpdateWorkflowExecutionWithRangeID(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		nil,
		nil,
		s.ShardInfo.RangeID,
		condition,
		nil,
		nil,
		nil,
		nil,
		nil,
		upsertChildInfos,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

// UpsertRequestCancelState is a utility method to update mutable state of workflow execution
func (s *TestBase) UpsertRequestCancelState(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	condition int64,
	upsertCancelInfos []*p.RequestCancelInfo,
) error {

	return s.UpdateWorkflowExecutionWithRangeID(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		nil,
		nil,
		s.ShardInfo.RangeID,
		condition,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		upsertCancelInfos,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

// UpsertSignalInfoState is a utility method to update mutable state of workflow execution
func (s *TestBase) UpsertSignalInfoState(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	condition int64,
	upsertSignalInfos []*p.SignalInfo,
) error {

	return s.UpdateWorkflowExecutionWithRangeID(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		nil,
		nil,
		s.ShardInfo.RangeID,
		condition,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		upsertSignalInfos,
		nil,
		nil,
		nil,
	)
}

// UpsertSignalsRequestedState is a utility method to update mutable state of workflow execution
func (s *TestBase) UpsertSignalsRequestedState(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	condition int64,
	upsertSignalsRequested []string,
) error {
	return s.UpdateWorkflowExecutionWithRangeID(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		nil,
		nil,
		s.ShardInfo.RangeID,
		condition,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		upsertSignalsRequested,
		nil,
	)
}

// DeleteChildExecutionsState is a utility method to delete child execution from mutable state
func (s *TestBase) DeleteChildExecutionsState(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	condition int64,
	deleteChildInfo int64,
) error {
	return s.UpdateWorkflowExecutionWithRangeID(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		nil,
		nil,
		s.ShardInfo.RangeID,
		condition,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		[]int64{deleteChildInfo},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

// DeleteCancelState is a utility method to delete request cancel state from mutable state
func (s *TestBase) DeleteCancelState(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	condition int64,
	deleteCancelInfo int64,
) error {
	return s.UpdateWorkflowExecutionWithRangeID(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		nil,
		nil,
		s.ShardInfo.RangeID,
		condition,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		[]int64{deleteCancelInfo},
		nil,
		nil,
		nil,
		nil,
	)
}

// DeleteSignalState is a utility method to delete request cancel state from mutable state
func (s *TestBase) DeleteSignalState(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	condition int64,
	deleteSignalInfo int64,
) error {
	return s.UpdateWorkflowExecutionWithRangeID(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		nil,
		nil,
		s.ShardInfo.RangeID,
		condition,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		[]int64{deleteSignalInfo},
		nil,
		nil,
	)
}

// DeleteSignalsRequestedState is a utility method to delete mutable state of workflow execution
func (s *TestBase) DeleteSignalsRequestedState(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	condition int64,
	deleteSignalsRequestedIDs []string,
) error {
	return s.UpdateWorkflowExecutionWithRangeID(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		nil,
		nil,
		s.ShardInfo.RangeID,
		condition,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		deleteSignalsRequestedIDs,
	)
}

// UpdateWorklowStateAndReplication is a utility method to update workflow execution
func (s *TestBase) UpdateWorklowStateAndReplication(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	condition int64,
	txTasks []p.Task,
) error {

	return s.UpdateWorkflowExecutionWithReplication(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		nil,
		nil,
		s.ShardInfo.RangeID,
		condition,
		nil,
		txTasks,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

// UpdateWorkflowExecutionWithRangeID is a utility method to update workflow execution
func (s *TestBase) UpdateWorkflowExecutionWithRangeID(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	decisionScheduleIDs []int64,
	activityScheduleIDs []int64,
	rangeID int64,
	condition int64,
	timerTasks []p.Task,
	upsertActivityInfos []*p.ActivityInfo,
	deleteActivityInfos []int64,
	upsertTimerInfos []*p.TimerInfo,
	deleteTimerInfos []string,
	upsertChildInfos []*p.ChildExecutionInfo,
	deleteChildInfos []int64,
	upsertCancelInfos []*p.RequestCancelInfo,
	deleteCancelInfos []int64,
	upsertSignalInfos []*p.SignalInfo,
	deleteSignalInfos []int64,
	upsertSignalRequestedIDs []string,
	deleteSignalRequestedIDs []string,
) error {
	return s.UpdateWorkflowExecutionWithReplication(
		ctx,
		updatedInfo,
		updatedStats,
		updatedVersionHistories,
		decisionScheduleIDs,
		activityScheduleIDs,
		rangeID,
		condition,
		timerTasks,
		[]p.Task{},
		upsertActivityInfos,
		deleteActivityInfos,
		upsertTimerInfos,
		deleteTimerInfos,
		upsertChildInfos,
		deleteChildInfos,
		upsertCancelInfos,
		deleteCancelInfos,
		upsertSignalInfos,
		deleteSignalInfos,
		upsertSignalRequestedIDs,
		deleteSignalRequestedIDs,
	)
}

// UpdateWorkflowExecutionWithReplication is a utility method to update workflow execution
func (s *TestBase) UpdateWorkflowExecutionWithReplication(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	updatedVersionHistories *p.VersionHistories,
	decisionScheduleIDs []int64,
	activityScheduleIDs []int64,
	rangeID int64,
	condition int64,
	timerTasks []p.Task,
	txTasks []p.Task,
	upsertActivityInfos []*p.ActivityInfo,
	deleteActivityInfos []int64,
	upsertTimerInfos []*p.TimerInfo,
	deleteTimerInfos []string,
	upsertChildInfos []*p.ChildExecutionInfo,
	deleteChildInfos []int64,
	upsertCancelInfos []*p.RequestCancelInfo,
	deleteCancelInfos []int64,
	upsertSignalInfos []*p.SignalInfo,
	deleteSignalInfos []int64,
	upsertSignalRequestedIDs []string,
	deleteSignalRequestedIDs []string,
) error {

	// TODO: use separate fields for those three task types
	var transferTasks []p.Task
	var crossClusterTasks []p.Task
	var replicationTasks []p.Task
	for _, task := range txTasks {
		switch t := task.(type) {
		case *p.DecisionTask,
			*p.ActivityTask,
			*p.CloseExecutionTask,
			*p.RecordWorkflowClosedTask,
			*p.RecordChildExecutionCompletedTask,
			*p.ApplyParentClosePolicyTask,
			*p.CancelExecutionTask,
			*p.StartChildExecutionTask,
			*p.SignalExecutionTask,
			*p.RecordWorkflowStartedTask,
			*p.ResetWorkflowTask,
			*p.UpsertWorkflowSearchAttributesTask:
			transferTasks = append(transferTasks, t)
		case *p.CrossClusterStartChildExecutionTask,
			*p.CrossClusterCancelExecutionTask,
			*p.CrossClusterSignalExecutionTask,
			*p.CrossClusterRecordChildExecutionCompletedTask,
			*p.CrossClusterApplyParentClosePolicyTask:
			crossClusterTasks = append(crossClusterTasks, t)
		case *p.HistoryReplicationTask, *p.SyncActivityTask:
			replicationTasks = append(replicationTasks, t)
		default:
			panic(fmt.Sprintf("Unknown transfer task type. %v", t))
		}
	}
	for _, decisionScheduleID := range decisionScheduleIDs {
		transferTasks = append(transferTasks, &p.DecisionTask{
			TaskID:     s.GetNextSequenceNumber(),
			DomainID:   updatedInfo.DomainID,
			TaskList:   updatedInfo.TaskList,
			ScheduleID: int64(decisionScheduleID)})
	}

	for _, activityScheduleID := range activityScheduleIDs {
		transferTasks = append(transferTasks, &p.ActivityTask{
			TaskID:     s.GetNextSequenceNumber(),
			DomainID:   updatedInfo.DomainID,
			TaskList:   updatedInfo.TaskList,
			ScheduleID: int64(activityScheduleID)})
	}
	_, err := s.ExecutionManager.UpdateWorkflowExecution(ctx, &p.UpdateWorkflowExecutionRequest{
		RangeID: rangeID,
		UpdateWorkflowMutation: p.WorkflowMutation{
			ExecutionInfo:    updatedInfo,
			ExecutionStats:   updatedStats,
			VersionHistories: updatedVersionHistories,

			UpsertActivityInfos:       upsertActivityInfos,
			DeleteActivityInfos:       deleteActivityInfos,
			UpsertTimerInfos:          upsertTimerInfos,
			DeleteTimerInfos:          deleteTimerInfos,
			UpsertChildExecutionInfos: upsertChildInfos,
			DeleteChildExecutionInfos: deleteChildInfos,
			UpsertRequestCancelInfos:  upsertCancelInfos,
			DeleteRequestCancelInfos:  deleteCancelInfos,
			UpsertSignalInfos:         upsertSignalInfos,
			DeleteSignalInfos:         deleteSignalInfos,
			UpsertSignalRequestedIDs:  upsertSignalRequestedIDs,
			DeleteSignalRequestedIDs:  deleteSignalRequestedIDs,

			TransferTasks:     transferTasks,
			CrossClusterTasks: crossClusterTasks,
			ReplicationTasks:  replicationTasks,
			TimerTasks:        timerTasks,

			Condition: condition,
			Checksum:  testWorkflowChecksum,
		},
		Encoding: pickRandomEncoding(),
	})
	return err
}

// UpdateWorkflowExecutionWithTransferTasks is a utility method to update workflow execution
func (s *TestBase) UpdateWorkflowExecutionWithTransferTasks(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	condition int64,
	transferTasks []p.Task,
	upsertActivityInfo []*p.ActivityInfo,
	versionHistories *p.VersionHistories,
) error {

	_, err := s.ExecutionManager.UpdateWorkflowExecution(ctx, &p.UpdateWorkflowExecutionRequest{
		UpdateWorkflowMutation: p.WorkflowMutation{
			ExecutionInfo:       updatedInfo,
			ExecutionStats:      updatedStats,
			TransferTasks:       transferTasks,
			Condition:           condition,
			UpsertActivityInfos: upsertActivityInfo,
			VersionHistories:    versionHistories,
		},
		RangeID:  s.ShardInfo.RangeID,
		Encoding: pickRandomEncoding(),
	})
	return err
}

// UpdateWorkflowExecutionForChildExecutionsInitiated is a utility method to update workflow execution
func (s *TestBase) UpdateWorkflowExecutionForChildExecutionsInitiated(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo, updatedStats *p.ExecutionStats, condition int64, transferTasks []p.Task, childInfos []*p.ChildExecutionInfo) error {
	_, err := s.ExecutionManager.UpdateWorkflowExecution(ctx, &p.UpdateWorkflowExecutionRequest{
		UpdateWorkflowMutation: p.WorkflowMutation{
			ExecutionInfo:             updatedInfo,
			ExecutionStats:            updatedStats,
			TransferTasks:             transferTasks,
			Condition:                 condition,
			UpsertChildExecutionInfos: childInfos,
		},
		RangeID:  s.ShardInfo.RangeID,
		Encoding: pickRandomEncoding(),
	})
	return err
}

// UpdateWorkflowExecutionForRequestCancel is a utility method to update workflow execution
func (s *TestBase) UpdateWorkflowExecutionForRequestCancel(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo, updatedStats *p.ExecutionStats, condition int64, transferTasks []p.Task,
	upsertRequestCancelInfo []*p.RequestCancelInfo) error {
	_, err := s.ExecutionManager.UpdateWorkflowExecution(ctx, &p.UpdateWorkflowExecutionRequest{
		UpdateWorkflowMutation: p.WorkflowMutation{
			ExecutionInfo:            updatedInfo,
			ExecutionStats:           updatedStats,
			TransferTasks:            transferTasks,
			Condition:                condition,
			UpsertRequestCancelInfos: upsertRequestCancelInfo,
		},
		RangeID:  s.ShardInfo.RangeID,
		Encoding: pickRandomEncoding(),
	})
	return err
}

// UpdateWorkflowExecutionForSignal is a utility method to update workflow execution
func (s *TestBase) UpdateWorkflowExecutionForSignal(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo, updatedStats *p.ExecutionStats, condition int64, transferTasks []p.Task,
	upsertSignalInfos []*p.SignalInfo) error {
	_, err := s.ExecutionManager.UpdateWorkflowExecution(ctx, &p.UpdateWorkflowExecutionRequest{
		UpdateWorkflowMutation: p.WorkflowMutation{
			ExecutionInfo:     updatedInfo,
			ExecutionStats:    updatedStats,
			TransferTasks:     transferTasks,
			Condition:         condition,
			UpsertSignalInfos: upsertSignalInfos,
		},
		RangeID:  s.ShardInfo.RangeID,
		Encoding: pickRandomEncoding(),
	})
	return err
}

// UpdateWorkflowExecutionForBufferEvents is a utility method to update workflow execution
func (s *TestBase) UpdateWorkflowExecutionForBufferEvents(
	ctx context.Context,
	updatedInfo *p.WorkflowExecutionInfo,
	updatedStats *p.ExecutionStats,
	condition int64,
	bufferEvents []*types.HistoryEvent,
	clearBufferedEvents bool,
	versionHistories *p.VersionHistories,
) error {

	_, err := s.ExecutionManager.UpdateWorkflowExecution(ctx, &p.UpdateWorkflowExecutionRequest{
		UpdateWorkflowMutation: p.WorkflowMutation{
			ExecutionInfo:       updatedInfo,
			ExecutionStats:      updatedStats,
			NewBufferedEvents:   bufferEvents,
			Condition:           condition,
			ClearBufferedEvents: clearBufferedEvents,
			VersionHistories:    versionHistories,
		},
		RangeID:  s.ShardInfo.RangeID,
		Encoding: pickRandomEncoding(),
	})
	return err
}

// UpdateAllMutableState is a utility method to update workflow execution
func (s *TestBase) UpdateAllMutableState(ctx context.Context, updatedMutableState *p.WorkflowMutableState, condition int64) error {
	var aInfos []*p.ActivityInfo
	for _, ai := range updatedMutableState.ActivityInfos {
		aInfos = append(aInfos, ai)
	}

	var tInfos []*p.TimerInfo
	for _, ti := range updatedMutableState.TimerInfos {
		tInfos = append(tInfos, ti)
	}

	var cInfos []*p.ChildExecutionInfo
	for _, ci := range updatedMutableState.ChildExecutionInfos {
		cInfos = append(cInfos, ci)
	}

	var rcInfos []*p.RequestCancelInfo
	for _, rci := range updatedMutableState.RequestCancelInfos {
		rcInfos = append(rcInfos, rci)
	}

	var sInfos []*p.SignalInfo
	for _, si := range updatedMutableState.SignalInfos {
		sInfos = append(sInfos, si)
	}

	var srIDs []string
	for id := range updatedMutableState.SignalRequestedIDs {
		srIDs = append(srIDs, id)
	}
	_, err := s.ExecutionManager.UpdateWorkflowExecution(ctx, &p.UpdateWorkflowExecutionRequest{
		RangeID: s.ShardInfo.RangeID,
		UpdateWorkflowMutation: p.WorkflowMutation{
			ExecutionInfo:             updatedMutableState.ExecutionInfo,
			ExecutionStats:            updatedMutableState.ExecutionStats,
			Condition:                 condition,
			UpsertActivityInfos:       aInfos,
			UpsertTimerInfos:          tInfos,
			UpsertChildExecutionInfos: cInfos,
			UpsertRequestCancelInfos:  rcInfos,
			UpsertSignalInfos:         sInfos,
			UpsertSignalRequestedIDs:  srIDs,
			VersionHistories:          updatedMutableState.VersionHistories,
		},
		Encoding: pickRandomEncoding(),
	})
	return err
}

// ConflictResolveWorkflowExecution is  utility method to reset mutable state
func (s *TestBase) ConflictResolveWorkflowExecution(
	ctx context.Context,
	info *p.WorkflowExecutionInfo,
	stats *p.ExecutionStats,
	nextEventID int64,
	activityInfos []*p.ActivityInfo,
	timerInfos []*p.TimerInfo,
	childExecutionInfos []*p.ChildExecutionInfo,
	requestCancelInfos []*p.RequestCancelInfo,
	signalInfos []*p.SignalInfo,
	ids []string,
	versionHistories *p.VersionHistories,
) error {

	_, err := s.ExecutionManager.ConflictResolveWorkflowExecution(ctx, &p.ConflictResolveWorkflowExecutionRequest{
		RangeID: s.ShardInfo.RangeID,
		ResetWorkflowSnapshot: p.WorkflowSnapshot{
			ExecutionInfo:       info,
			ExecutionStats:      stats,
			Condition:           nextEventID,
			ActivityInfos:       activityInfos,
			TimerInfos:          timerInfos,
			ChildExecutionInfos: childExecutionInfos,
			RequestCancelInfos:  requestCancelInfos,
			SignalInfos:         signalInfos,
			SignalRequestedIDs:  ids,
			Checksum:            testWorkflowChecksum,
			VersionHistories:    versionHistories,
		},
		Encoding: pickRandomEncoding(),
	})
	return err
}

// DeleteWorkflowExecution is a utility method to delete a workflow execution
func (s *TestBase) DeleteWorkflowExecution(ctx context.Context, info *p.WorkflowExecutionInfo) error {
	return s.ExecutionManager.DeleteWorkflowExecution(ctx, &p.DeleteWorkflowExecutionRequest{
		DomainID:   info.DomainID,
		WorkflowID: info.WorkflowID,
		RunID:      info.RunID,
	})
}

// DeleteCurrentWorkflowExecution is a utility method to delete the workflow current execution
func (s *TestBase) DeleteCurrentWorkflowExecution(ctx context.Context, info *p.WorkflowExecutionInfo) error {
	return s.ExecutionManager.DeleteCurrentWorkflowExecution(ctx, &p.DeleteCurrentWorkflowExecutionRequest{
		DomainID:   info.DomainID,
		WorkflowID: info.WorkflowID,
		RunID:      info.RunID,
	})
}

// GetTransferTasks is a utility method to get tasks from transfer task queue
func (s *TestBase) GetTransferTasks(ctx context.Context, batchSize int, getAll bool) ([]*p.TransferTaskInfo, error) {
	result := []*p.TransferTaskInfo{}
	var token []byte

Loop:
	for {
		response, err := s.ExecutionManager.GetTransferTasks(ctx, &p.GetTransferTasksRequest{
			ReadLevel:     0,
			MaxReadLevel:  math.MaxInt64,
			BatchSize:     batchSize,
			NextPageToken: token,
		})
		if err != nil {
			return nil, err
		}

		token = response.NextPageToken
		result = append(result, response.Tasks...)
		if len(token) == 0 || !getAll {
			break Loop
		}
	}

	return result, nil
}

// GetCrossClusterTasks is a utility method to get tasks from transfer task queue
func (s *TestBase) GetCrossClusterTasks(ctx context.Context, targetCluster string, readLevel int64, batchSize int, getAll bool) ([]*p.CrossClusterTaskInfo, error) {
	result := []*p.CrossClusterTaskInfo{}
	var token []byte

	for {
		response, err := s.ExecutionManager.GetCrossClusterTasks(ctx, &p.GetCrossClusterTasksRequest{
			TargetCluster: targetCluster,
			ReadLevel:     readLevel,
			MaxReadLevel:  int64(math.MaxInt64),
			BatchSize:     batchSize,
			NextPageToken: token,
		})
		if err != nil {
			return nil, err
		}

		token = response.NextPageToken
		result = append(result, response.Tasks...)
		if len(response.NextPageToken) == 0 || !getAll {
			break
		}
	}

	return result, nil
}

// GetReplicationTasks is a utility method to get tasks from replication task queue
func (s *TestBase) GetReplicationTasks(ctx context.Context, batchSize int, getAll bool) ([]*p.ReplicationTaskInfo, error) {
	result := []*p.ReplicationTaskInfo{}
	var token []byte

Loop:
	for {
		response, err := s.ExecutionManager.GetReplicationTasks(ctx, &p.GetReplicationTasksRequest{
			ReadLevel:     0,
			MaxReadLevel:  math.MaxInt64,
			BatchSize:     batchSize,
			NextPageToken: token,
		})
		if err != nil {
			return nil, err
		}

		token = response.NextPageToken
		result = append(result, response.Tasks...)
		if len(token) == 0 || !getAll {
			break Loop
		}
	}

	return result, nil
}

// RangeCompleteReplicationTask is a utility method to complete a range of replication tasks
func (s *TestBase) RangeCompleteReplicationTask(ctx context.Context, inclusiveEndTaskID int64) error {
	for {
		resp, err := s.ExecutionManager.RangeCompleteReplicationTask(ctx, &p.RangeCompleteReplicationTaskRequest{
			InclusiveEndTaskID: inclusiveEndTaskID,
			PageSize:           1,
		})
		if err != nil {
			return err
		}
		if !p.HasMoreRowsToDelete(resp.TasksCompleted, 1) {
			break
		}
	}
	return nil
}

// PutReplicationTaskToDLQ is a utility method to insert a replication task info
func (s *TestBase) PutReplicationTaskToDLQ(
	ctx context.Context,
	sourceCluster string,
	taskInfo *p.ReplicationTaskInfo,
) error {

	return s.ExecutionManager.PutReplicationTaskToDLQ(ctx, &p.PutReplicationTaskToDLQRequest{
		SourceClusterName: sourceCluster,
		TaskInfo:          taskInfo,
	})
}

// GetReplicationTasksFromDLQ is a utility method to read replication task info
func (s *TestBase) GetReplicationTasksFromDLQ(
	ctx context.Context,
	sourceCluster string,
	readLevel int64,
	maxReadLevel int64,
	pageSize int,
	pageToken []byte,
) (*p.GetReplicationTasksFromDLQResponse, error) {

	return s.ExecutionManager.GetReplicationTasksFromDLQ(ctx, &p.GetReplicationTasksFromDLQRequest{
		SourceClusterName: sourceCluster,
		GetReplicationTasksRequest: p.GetReplicationTasksRequest{
			ReadLevel:     readLevel,
			MaxReadLevel:  maxReadLevel,
			BatchSize:     pageSize,
			NextPageToken: pageToken,
		},
	})
}

// GetReplicationDLQSize is a utility method to read replication dlq size
func (s *TestBase) GetReplicationDLQSize(
	ctx context.Context,
	sourceCluster string,
) (*p.GetReplicationDLQSizeResponse, error) {

	return s.ExecutionManager.GetReplicationDLQSize(ctx, &p.GetReplicationDLQSizeRequest{
		SourceClusterName: sourceCluster,
	})
}

// DeleteReplicationTaskFromDLQ is a utility method to delete a replication task info
func (s *TestBase) DeleteReplicationTaskFromDLQ(
	ctx context.Context,
	sourceCluster string,
	taskID int64,
) error {

	return s.ExecutionManager.DeleteReplicationTaskFromDLQ(ctx, &p.DeleteReplicationTaskFromDLQRequest{
		SourceClusterName: sourceCluster,
		TaskID:            taskID,
	})
}

// RangeDeleteReplicationTaskFromDLQ is a utility method to delete  replication task info
func (s *TestBase) RangeDeleteReplicationTaskFromDLQ(
	ctx context.Context,
	sourceCluster string,
	beginTaskID int64,
	endTaskID int64,
) error {

	_, err := s.ExecutionManager.RangeDeleteReplicationTaskFromDLQ(ctx, &p.RangeDeleteReplicationTaskFromDLQRequest{
		SourceClusterName:    sourceCluster,
		ExclusiveBeginTaskID: beginTaskID,
		InclusiveEndTaskID:   endTaskID,
	})
	return err
}

// CreateFailoverMarkers is a utility method to create failover markers
func (s *TestBase) CreateFailoverMarkers(
	ctx context.Context,
	markers []*p.FailoverMarkerTask,
) error {

	return s.ExecutionManager.CreateFailoverMarkerTasks(ctx, &p.CreateFailoverMarkersRequest{
		RangeID: s.ShardInfo.RangeID,
		Markers: markers,
	})
}

// CompleteTransferTask is a utility method to complete a transfer task
func (s *TestBase) CompleteTransferTask(ctx context.Context, taskID int64) error {

	return s.ExecutionManager.CompleteTransferTask(ctx, &p.CompleteTransferTaskRequest{
		TaskID: taskID,
	})
}

// RangeCompleteTransferTask is a utility method to complete a range of transfer tasks
func (s *TestBase) RangeCompleteTransferTask(ctx context.Context, exclusiveBeginTaskID int64, inclusiveEndTaskID int64) error {
	for {
		resp, err := s.ExecutionManager.RangeCompleteTransferTask(ctx, &p.RangeCompleteTransferTaskRequest{
			ExclusiveBeginTaskID: exclusiveBeginTaskID,
			InclusiveEndTaskID:   inclusiveEndTaskID,
			PageSize:             1,
		})
		if err != nil {
			return err
		}
		if !p.HasMoreRowsToDelete(resp.TasksCompleted, 1) {
			break
		}
	}
	return nil
}

// CompleteCrossClusterTask is a utility method to complete a cross-cluster task
func (s *TestBase) CompleteCrossClusterTask(ctx context.Context, targetCluster string, taskID int64) error {
	return s.ExecutionManager.CompleteCrossClusterTask(ctx, &p.CompleteCrossClusterTaskRequest{
		TargetCluster: targetCluster,
		TaskID:        taskID,
	})
}

// RangeCompleteCrossClusterTask is a utility method to complete a range of cross-cluster tasks
func (s *TestBase) RangeCompleteCrossClusterTask(ctx context.Context, targetCluster string, exclusiveBeginTaskID int64, inclusiveEndTaskID int64) error {
	for {
		resp, err := s.ExecutionManager.RangeCompleteCrossClusterTask(ctx, &p.RangeCompleteCrossClusterTaskRequest{
			TargetCluster:        targetCluster,
			ExclusiveBeginTaskID: exclusiveBeginTaskID,
			InclusiveEndTaskID:   inclusiveEndTaskID,
			PageSize:             1,
		})
		if err != nil {
			return err
		}
		if !p.HasMoreRowsToDelete(resp.TasksCompleted, 1) {
			break
		}
	}
	return nil
}

// CompleteReplicationTask is a utility method to complete a replication task
func (s *TestBase) CompleteReplicationTask(ctx context.Context, taskID int64) error {

	return s.ExecutionManager.CompleteReplicationTask(ctx, &p.CompleteReplicationTaskRequest{
		TaskID: taskID,
	})
}

// GetTimerIndexTasks is a utility method to get tasks from transfer task queue
func (s *TestBase) GetTimerIndexTasks(ctx context.Context, batchSize int, getAll bool) ([]*p.TimerTaskInfo, error) {
	result := []*p.TimerTaskInfo{}
	var token []byte

Loop:
	for {
		response, err := s.ExecutionManager.GetTimerIndexTasks(ctx, &p.GetTimerIndexTasksRequest{
			MinTimestamp:  time.Time{},
			MaxTimestamp:  time.Unix(0, math.MaxInt64),
			BatchSize:     batchSize,
			NextPageToken: token,
		})
		if err != nil {
			return nil, err
		}

		token = response.NextPageToken
		result = append(result, response.Timers...)
		if len(token) == 0 || !getAll {
			break Loop
		}
	}

	return result, nil
}

// CompleteTimerTask is a utility method to complete a timer task
func (s *TestBase) CompleteTimerTask(ctx context.Context, ts time.Time, taskID int64) error {
	return s.ExecutionManager.CompleteTimerTask(ctx, &p.CompleteTimerTaskRequest{
		VisibilityTimestamp: ts,
		TaskID:              taskID,
	})
}

// RangeCompleteTimerTask is a utility method to complete a range of timer tasks
func (s *TestBase) RangeCompleteTimerTask(ctx context.Context, inclusiveBeginTimestamp time.Time, exclusiveEndTimestamp time.Time) error {
	for {
		resp, err := s.ExecutionManager.RangeCompleteTimerTask(ctx, &p.RangeCompleteTimerTaskRequest{
			InclusiveBeginTimestamp: inclusiveBeginTimestamp,
			ExclusiveEndTimestamp:   exclusiveEndTimestamp,
			PageSize:                1,
		})
		if err != nil {
			return err
		}
		if !p.HasMoreRowsToDelete(resp.TasksCompleted, 1) {
			break
		}
	}
	return nil
}

// CreateDecisionTask is a utility method to create a task
func (s *TestBase) CreateDecisionTask(ctx context.Context, domainID string, workflowExecution types.WorkflowExecution, taskList string,
	decisionScheduleID int64) (int64, error) {
	leaseResponse, err := s.TaskMgr.LeaseTaskList(ctx, &p.LeaseTaskListRequest{
		DomainID: domainID,
		TaskList: taskList,
		TaskType: p.TaskListTypeDecision,
	})
	if err != nil {
		return 0, err
	}

	taskID := s.GetNextSequenceNumber()
	tasks := []*p.CreateTaskInfo{
		{
			TaskID:    taskID,
			Execution: workflowExecution,
			Data: &p.TaskInfo{
				DomainID:   domainID,
				WorkflowID: workflowExecution.WorkflowID,
				RunID:      workflowExecution.RunID,
				TaskID:     taskID,
				ScheduleID: decisionScheduleID,
			},
		},
	}

	_, err = s.TaskMgr.CreateTasks(ctx, &p.CreateTasksRequest{
		TaskListInfo: leaseResponse.TaskListInfo,
		Tasks:        tasks,
	})

	if err != nil {
		return 0, err
	}

	return taskID, err
}

// CreateActivityTasks is a utility method to create tasks
func (s *TestBase) CreateActivityTasks(ctx context.Context, domainID string, workflowExecution types.WorkflowExecution,
	activities map[int64]string) ([]int64, error) {

	taskLists := make(map[string]*p.TaskListInfo)
	for _, tl := range activities {
		_, ok := taskLists[tl]
		if !ok {
			resp, err := s.TaskMgr.LeaseTaskList(
				ctx,
				&p.LeaseTaskListRequest{DomainID: domainID, TaskList: tl, TaskType: p.TaskListTypeActivity})
			if err != nil {
				return []int64{}, err
			}
			taskLists[tl] = resp.TaskListInfo
		}
	}

	var taskIDs []int64
	for activityScheduleID, taskList := range activities {
		taskID := s.GetNextSequenceNumber()
		tasks := []*p.CreateTaskInfo{
			{
				TaskID:    taskID,
				Execution: workflowExecution,
				Data: &p.TaskInfo{
					DomainID:               domainID,
					WorkflowID:             workflowExecution.WorkflowID,
					RunID:                  workflowExecution.RunID,
					TaskID:                 taskID,
					ScheduleID:             activityScheduleID,
					ScheduleToStartTimeout: defaultScheduleToStartTimeout,
				},
			},
		}
		_, err := s.TaskMgr.CreateTasks(ctx, &p.CreateTasksRequest{
			TaskListInfo: taskLists[taskList],
			Tasks:        tasks,
		})
		if err != nil {
			return nil, err
		}
		taskIDs = append(taskIDs, taskID)
	}

	return taskIDs, nil
}

// GetTasks is a utility method to get tasks from persistence
func (s *TestBase) GetTasks(ctx context.Context, domainID, taskList string, taskType int, batchSize int) (*p.GetTasksResponse, error) {
	response, err := s.TaskMgr.GetTasks(ctx, &p.GetTasksRequest{
		DomainID:     domainID,
		TaskList:     taskList,
		TaskType:     taskType,
		BatchSize:    batchSize,
		MaxReadLevel: common.Int64Ptr(math.MaxInt64),
	})

	if err != nil {
		return nil, err
	}

	return &p.GetTasksResponse{Tasks: response.Tasks}, nil
}

// CompleteTask is a utility method to complete a task
func (s *TestBase) CompleteTask(ctx context.Context, domainID, taskList string, taskType int, taskID int64, ackLevel int64) error {
	return s.TaskMgr.CompleteTask(ctx, &p.CompleteTaskRequest{
		TaskList: &p.TaskListInfo{
			DomainID: domainID,
			AckLevel: ackLevel,
			TaskType: taskType,
			Name:     taskList,
		},
		TaskID: taskID,
	})
}

// TearDownWorkflowStore to cleanup
func (s *TestBase) TearDownWorkflowStore() {
	s.ExecutionMgrFactory.Close()

	s.DefaultTestCluster.TearDownTestDatabase()
}

// GetNextSequenceNumber generates a unique sequence number for can be used for transfer queue taskId
func (s *TestBase) GetNextSequenceNumber() int64 {
	taskID, _ := s.TaskIDGenerator.GenerateTransferTaskID()
	return taskID
}

// ClearTasks completes all transfer tasks and replication tasks
func (s *TestBase) ClearTasks() {
	s.ClearTransferQueue()
	s.ClearReplicationQueue()
}

// ClearTransferQueue completes all tasks in transfer queue
func (s *TestBase) ClearTransferQueue() {
	s.Logger.Info("Clearing transfer tasks", tag.ShardRangeID(s.ShardInfo.RangeID))
	tasks, err := s.GetTransferTasks(context.Background(), 100, true)
	if err != nil {
		s.Logger.Fatal("Error during cleanup", tag.Error(err))
	}

	counter := 0
	for _, t := range tasks {
		s.Logger.Info("Deleting transfer task with ID", tag.TaskID(t.TaskID))
		s.NoError(s.CompleteTransferTask(context.Background(), t.TaskID))
		counter++
	}

	s.Logger.Info("Deleted transfer tasks.", tag.Counter(counter))
}

// ClearReplicationQueue completes all tasks in replication queue
func (s *TestBase) ClearReplicationQueue() {
	s.Logger.Info("Clearing replication tasks", tag.ShardRangeID(s.ShardInfo.RangeID))
	tasks, err := s.GetReplicationTasks(context.Background(), 100, true)
	if err != nil {
		s.Logger.Fatal("Error during cleanup", tag.Error(err))
	}

	counter := 0
	for _, t := range tasks {
		s.Logger.Info("Deleting replication task with ID", tag.TaskID(t.TaskID))
		s.NoError(s.CompleteReplicationTask(context.Background(), t.TaskID))
		counter++
	}

	s.Logger.Info("Deleted replication tasks.", tag.Counter(counter))
}

// EqualTimesWithPrecision assertion that two times are equal within precision
func (s *TestBase) EqualTimesWithPrecision(t1, t2 time.Time, precision time.Duration) {
	s.True(timeComparator(t1, t2, precision),
		"Not equal: \n"+
			"expected: %s\n"+
			"actual  : %s%s", t1, t2,
	)
}

// EqualTimes assertion that two times are equal within two millisecond precision
func (s *TestBase) EqualTimes(t1, t2 time.Time) {
	s.EqualTimesWithPrecision(t1, t2, TimePrecision)
}

func (s *TestBase) validateTimeRange(t time.Time, expectedDuration time.Duration) bool {
	currentTime := time.Now()
	diff := time.Duration(currentTime.UnixNano() - t.UnixNano())
	if diff > expectedDuration {
		s.Logger.Info("Check Current time, Application time, Difference", tag.Timestamp(t), tag.CursorTimestamp(currentTime), tag.Number(int64(diff)))
		return false
	}
	return true
}

// GenerateTransferTaskID helper
func (g *TestTransferTaskIDGenerator) GenerateTransferTaskID() (int64, error) {
	return atomic.AddInt64(&g.seqNum, 1), nil
}

// Publish is a utility method to add messages to the queue
func (s *TestBase) Publish(
	ctx context.Context,
	messagePayload []byte,
) error {

	retryPolicy := backoff.NewExponentialRetryPolicy(100 * time.Millisecond)
	retryPolicy.SetBackoffCoefficient(1.5)
	retryPolicy.SetMaximumAttempts(5)

	throttleRetry := backoff.NewThrottleRetry(
		backoff.WithRetryPolicy(retryPolicy),
		backoff.WithRetryableError(func(e error) bool {
			return persistence.IsTransientError(e) || isMessageIDConflictError(e)
		}),
	)
	return throttleRetry.Do(ctx, func() error {
		return s.DomainReplicationQueueMgr.EnqueueMessage(ctx, messagePayload)
	})
}

func isMessageIDConflictError(err error) bool {
	_, ok := err.(*p.ConditionFailedError)
	return ok
}

// GetReplicationMessages is a utility method to get messages from the queue
func (s *TestBase) GetReplicationMessages(
	ctx context.Context,
	lastMessageID int64,
	maxCount int,
) ([]*p.QueueMessage, error) {

	return s.DomainReplicationQueueMgr.ReadMessages(ctx, lastMessageID, maxCount)
}

// UpdateAckLevel updates replication queue ack level
func (s *TestBase) UpdateAckLevel(
	ctx context.Context,
	lastProcessedMessageID int64,
	clusterName string,
) error {

	return s.DomainReplicationQueueMgr.UpdateAckLevel(ctx, lastProcessedMessageID, clusterName)
}

// GetAckLevels returns replication queue ack levels
func (s *TestBase) GetAckLevels(
	ctx context.Context,
) (map[string]int64, error) {
	return s.DomainReplicationQueueMgr.GetAckLevels(ctx)
}

// PublishToDomainDLQ is a utility method to add messages to the domain DLQ
func (s *TestBase) PublishToDomainDLQ(
	ctx context.Context,
	messagePayload []byte,
) error {

	retryPolicy := backoff.NewExponentialRetryPolicy(100 * time.Millisecond)
	retryPolicy.SetBackoffCoefficient(1.5)
	retryPolicy.SetMaximumAttempts(5)

	throttleRetry := backoff.NewThrottleRetry(
		backoff.WithRetryPolicy(retryPolicy),
		backoff.WithRetryableError(func(e error) bool {
			return persistence.IsTransientError(e) || isMessageIDConflictError(e)
		}),
	)
	return throttleRetry.Do(ctx, func() error {
		return s.DomainReplicationQueueMgr.EnqueueMessageToDLQ(ctx, messagePayload)
	})
}

// GetMessagesFromDomainDLQ is a utility method to get messages from the domain DLQ
func (s *TestBase) GetMessagesFromDomainDLQ(
	ctx context.Context,
	firstMessageID int64,
	lastMessageID int64,
	pageSize int,
	pageToken []byte,
) ([]*p.QueueMessage, []byte, error) {

	return s.DomainReplicationQueueMgr.ReadMessagesFromDLQ(
		ctx,
		firstMessageID,
		lastMessageID,
		pageSize,
		pageToken,
	)
}

// UpdateDomainDLQAckLevel updates domain dlq ack level
func (s *TestBase) UpdateDomainDLQAckLevel(
	ctx context.Context,
	lastProcessedMessageID int64,
	clusterName string,
) error {

	return s.DomainReplicationQueueMgr.UpdateDLQAckLevel(ctx, lastProcessedMessageID, clusterName)
}

// GetDomainDLQAckLevel returns domain dlq ack level
func (s *TestBase) GetDomainDLQAckLevel(
	ctx context.Context,
) (map[string]int64, error) {
	return s.DomainReplicationQueueMgr.GetDLQAckLevels(ctx)
}

// GetDomainDLQSize returns domain dlq size
func (s *TestBase) GetDomainDLQSize(
	ctx context.Context,
) (int64, error) {
	return s.DomainReplicationQueueMgr.GetDLQSize(ctx)
}

// DeleteMessageFromDomainDLQ deletes one message from domain DLQ
func (s *TestBase) DeleteMessageFromDomainDLQ(
	ctx context.Context,
	messageID int64,
) error {

	return s.DomainReplicationQueueMgr.DeleteMessageFromDLQ(ctx, messageID)
}

// RangeDeleteMessagesFromDomainDLQ deletes messages from domain DLQ
func (s *TestBase) RangeDeleteMessagesFromDomainDLQ(
	ctx context.Context,
	firstMessageID int64,
	lastMessageID int64,
) error {

	return s.DomainReplicationQueueMgr.RangeDeleteMessagesFromDLQ(ctx, firstMessageID, lastMessageID)
}

// GenerateTransferTaskIDs helper
func (g *TestTransferTaskIDGenerator) GenerateTransferTaskIDs(number int) ([]int64, error) {
	result := []int64{}
	for i := 0; i < number; i++ {
		id, err := g.GenerateTransferTaskID()
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}

// GenerateRandomDBName helper
func GenerateRandomDBName(n int) string {
	rand.Seed(time.Now().UnixNano())
	letterRunes := []rune("workflow")
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func pickRandomEncoding() common.EncodingType {
	// randomly pick json/thriftrw/empty as encoding type
	var encoding common.EncodingType
	i := rand.Intn(3)
	switch i {
	case 0:
		encoding = common.EncodingTypeJSON
	case 1:
		encoding = common.EncodingTypeThriftRW
	case 2:
		encoding = common.EncodingType("")
	}
	return encoding
}

func int64Ptr(i int64) *int64 {
	return &i
}
