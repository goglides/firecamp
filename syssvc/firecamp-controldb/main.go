package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/golang/glog"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/containersvc/awsecs"
	"github.com/cloudstax/firecamp/containersvc/swarm"
	"github.com/cloudstax/firecamp/db/controldb/server"
	"github.com/cloudstax/firecamp/utils"
)

var (
	platform = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	dataDir  = flag.String("datadir", common.ControlDBDefaultDir, "The DBService server stores data under the dir")
	tls      = flag.Bool("tlsverify", false, "Connection uses TLS if true, else plain TCP")
	certFile = flag.String("tlscert", "", "The TLS cert file")
	keyFile  = flag.String("tlskey", "", "The TLS key file")
)

func main() {
	flag.Parse()

	// log to std, the manageserver container will send the logs to such as AWS CloudWatch.
	utils.SetLogToStd()
	//utils.SetLogDir()

	if *tls && (*certFile == "" || *keyFile == "") {
		fmt.Println("invalid command, please pass cert file and key file for tls", *certFile, *keyFile)
		os.Exit(-1)
	}

	var info containersvc.Info
	var err error
	switch *platform {
	case common.ContainerPlatformECS:
		info, err = awsecs.NewEcsInfo()
		if err != nil {
			glog.Fatalln("NewEcsInfo error", err)
		}

	case common.ContainerPlatformSwarm:
		cluster := os.Getenv(common.ENV_CLUSTER)
		if len(cluster) == 0 {
			glog.Fatalln("cluster name env is not set for swarm")
		}

		info, err = swarmsvc.NewSwarmInfo(cluster)
		if err != nil {
			glog.Fatalln("NewSwarmInfo error", err)
		}

	default:
		glog.Fatalln("unsupport container platform", *platform)
	}

	controldbserver.StartControlDBServer(info.GetContainerClusterID(), *dataDir, *tls, *certFile, *keyFile)
}
