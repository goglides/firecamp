package awsecs

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/log/jsonfile"
	"github.com/cloudstax/firecamp/utils"
)

func TestTaskDef(t *testing.T) {
	cfg := aws.NewConfig().WithRegion("us-west-1")
	sess, err := session.NewSession(cfg)
	if err != nil {
		t.Fatalf("failed to create session, error %s", err)
	}

	e := NewAWSEcs(sess)

	// create context
	ctx := context.Background()

	commonOpts := &containersvc.CommonOptions{
		Cluster:        "test-cluster",
		ServiceName:    "test-service",
		ServiceUUID:    "test-serviceUUID",
		ContainerImage: containersvc.TestBusyBoxContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: int64(0),
			MaxMemMB:        int64(128),
			ReserveMemMB:    int64(16),
		},
		LogConfig: jsonfilelog.CreateJSONFileLogConfig(),
	}

	opts := &containersvc.RunTaskOptions{
		Common:   commonOpts,
		TaskType: common.TaskTypeInit,
	}

	uuid := utils.GenUUID()
	taskDefFamily := "testTaskDef-" + uuid

	// create the task def
	taskDef, exist, err := e.createEcsTaskDefinitionForTask(ctx, taskDefFamily, opts)
	if err != nil {
		t.Fatalf("createEcsTaskDefinitionForTask %s error %s", taskDefFamily, err)
	}
	if exist {
		t.Fatalf("taskDefFamily %s exists", taskDefFamily)
	}

	// taskdef should exist
	exist, err = e.isTaskDefFamilyExist(ctx, taskDefFamily)
	if err != nil {
		t.Fatalf("isTaskDefFamilyExist %s error %s", taskDefFamily, err)
	}
	if !exist {
		t.Fatalf("taskDefFamily %s not exists", taskDefFamily)
	}

	taskDef1, err := e.getLatestTaskDefinition(ctx, taskDefFamily)
	if err != nil {
		t.Fatalf("getLatestTaskDefinition %s error %s", taskDefFamily, err)
	}
	if taskDef != taskDef1 {
		t.Fatalf("expect task definition %s got %s", taskDef, taskDef1)
	}

	// test createEcsTaskDefinitionForTask for the created taskDef
	taskDef2, exist, err := e.createEcsTaskDefinitionForTask(ctx, taskDefFamily, opts)
	if err != nil {
		t.Fatalf("createEcsTaskDefinitionForTask %s error %s", taskDefFamily, err)
	}
	if exist != true || taskDef != taskDef2 {
		t.Fatalf("createEcsTaskDefinitionForTask expect taskDef exist %d, get taskdef %s expect %s", exist, taskDef, taskDef2)
	}

	// deregisterTaskDefinition
	err = e.deregisterTaskDefinition(ctx, taskDef)
	if err != nil {
		t.Fatalf("DeregisterTaskDefinition %s error %s", taskDef, err)
	}
}

func TestService(t *testing.T) {
	cfg := aws.NewConfig().WithRegion("us-west-1")
	sess, err := session.NewSession(cfg)
	if err != nil {
		t.Fatalf("failed to create session, error %s", err)
	}

	e := NewAWSEcs(sess)

	// create context
	ctx := context.Background()

	// create ECS cluster
	cluster := "cluster-" + utils.GenUUID()
	err = e.CreateCluster(ctx, cluster)
	if err != nil {
		t.Fatalf("create cluster %s error %s", cluster, err)
	}
	defer deleteCluster(ctx, t, e, cluster)

	// create services
	service := "test-service1"
	containerPath := ""
	serviceTest(ctx, t, e, cluster, service, containerPath, nil)

	service = "test-service2"
	containerPath = "/data"
	serviceTest(ctx, t, e, cluster, service, containerPath, nil)

	service = "test-service3"
	containerPath = "/data"
	place := &containersvc.Placement{
		Zones: []string{"us-east-1a", "us-east-1b"},
	}
	serviceTest(ctx, t, e, cluster, service, containerPath, place)
}

func serviceTest(ctx context.Context, t *testing.T, e *AWSEcs, cluster string, service string, containerPath string, place *containersvc.Placement) {
	exist, err := e.IsServiceExist(ctx, cluster, service)
	if err != nil {
		t.Fatalf("IsServiceExist error %s, cluster %s service %s", err, cluster, service)
	}
	if exist {
		t.Fatalf("service %s should not exist, cluster %s", service, cluster)
	}

	commonOpts := &containersvc.CommonOptions{
		Cluster:        cluster,
		ServiceName:    service,
		ServiceUUID:    service + "uuid",
		ContainerImage: containersvc.TestBusyBoxContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: int64(0),
			MaxMemMB:        int64(128),
			ReserveMemMB:    int64(16),
		},
		LogConfig: jsonfilelog.CreateJSONFileLogConfig(),
	}

	p := common.PortMapping{
		ContainerPort: 23011,
		HostPort:      23011,
	}

	opts := &containersvc.CreateServiceOptions{
		Common:       commonOpts,
		PortMappings: []common.PortMapping{p},
		Replicas:     int64(0),
		Place:        place,
	}
	if len(containerPath) != 0 {
		opts.DataVolume = &containersvc.VolumeOptions{
			MountPath: containerPath,
		}
	}

	err = e.CreateService(ctx, opts)
	if err != nil {
		t.Fatalf("CreateService error %s, %s", err, opts)
	}
	defer deleteService(ctx, t, e, cluster, service)

	exist, err = e.IsServiceExist(ctx, cluster, service)
	if err != nil {
		t.Fatalf("IsServiceExist error %s, cluster %s service %s", err, cluster, service)
	}
	if !exist {
		t.Fatalf("service %s should exist, cluster %s", service, cluster)
	}
}

func deleteCluster(ctx context.Context, t *testing.T, e *AWSEcs, cluster string) {
	err := e.DeleteCluster(ctx, cluster)
	if err != nil {
		t.Fatalf("DeleteCluster %s error %s", cluster, err)
	}
}

func deleteService(ctx context.Context, t *testing.T, e *AWSEcs, cluster string, service string) {
	err := e.DeleteService(ctx, cluster, service)
	if err != nil {
		t.Fatalf("DeleteService %s error %s, cluster %s", service, err, cluster)
	}
}
