#!/bin/bash -e

version=$1
clusterName=$2
containerPlatform=$3
containerPlatformRole=$4
azs=$5

if [ "$version" = "" -o "$clusterName" = "" -o "$azs" = "" ]; then
  echo "version $version, clusterName $clusterName and azs $azs should not be empty"
  exit 1
fi

if [ "$containerPlatform" != "ecs" -a "$containerPlatform" != "swarm" ]; then
  echo "invalid container platform $containerPlatform"
  exit 1
fi

if [ "$containerPlatformRole" != "worker" -a "$containerPlatformRole" != "manager" ]; then
  echo "invalid container platform role $containerPlatformRole"
  exit 1
fi

echo "init version $version, cluster $clusterName, container platform $containerPlatform, role $containerPlatformRole, azs $azs"

# 1. tune the system configs.
# set never for THP (Transparent Huge Pages), as THP might impact the performance for some services
# such as MongoDB and Reddis. Could set to madvise if some service really benefits from madvise.
# https://www.kernel.org/doc/Documentation/vm/transhuge.txt
echo never | sudo tee /sys/kernel/mm/transparent_hugepage/enabled

# increase somaxconn to 512 for such as Redis
echo "net.core.somaxconn=512" >> /etc/sysctl.conf
sysctl -w net.core.somaxconn=512
echo "net.ipv4.tcp_max_syn_backlog=512" >> /etc/sysctl.conf
sysctl -w net.ipv4.tcp_max_syn_backlog=512

# increase max_map_count to 262144 for ElasticSearch
# https://www.elastic.co/guide/en/elasticsearch/reference/current/docker.html
echo "vm.max_map_count=262144" >> /etc/sysctl.conf
sysctl -w vm.max_map_count=262144

# set vm.swappiness to 1 to avoid swapping for ElasticSearch. This could also benefit
# other services, as usually one node should only run one stateful service in production.
# https://en.wikipedia.org/wiki/Swappiness
echo "vm.swappiness=1" >> /etc/sysctl.conf
sysctl -w vm.swappiness=1

# set overcommit to 1 as required by Redis. Would not cause issue to other services
echo "vm.overcommit_memory=1" >> /etc/sysctl.conf
sysctl -w vm.overcommit_memory=1


# 2. install docker.
yum install -y docker


# 3. Container platform specific initialization.
if [ "$containerPlatform" = "ecs" ]; then
  # Kafka uses a very large number of files, increase the file descriptor count.
  # AWS AMI sets the ulimit for docker daemon, OPTIONS=\"--default-ulimit nofile=1024:4096\".
  # The container inherits the docker daemon ulimit.
  # The docker daemon config file is different on different Linux. AWS AMI is /etc/sysconfig/docker.
  # Ubuntu is /etc/init/docker.conf
  sed -i "s/OPTIONS=\"--default-ulimit.*/OPTIONS=\"--default-ulimit nofile=100000:100000 --default-ulimit nproc=64000:64000\"/g" /etc/sysconfig/docker

  service docker start

  sleep 3

  # follow https://github.com/aws/amazon-ecs-agent#usage to set up ecs agent
  # Set up directories the agent uses
  mkdir -p /var/log/ecs /etc/ecs /var/lib/ecs/data
  touch /etc/ecs/ecs.config

  # starting at 1.15, ecs agent requires to have the logging driver configured in ecs.config, or else ecs agent will fail.
  echo "ECS_AVAILABLE_LOGGING_DRIVERS=[\"json-file\",\"awslogs\"]" >> /etc/ecs/ecs.config
  echo "ECS_CLUSTER=$clusterName" >> /etc/ecs/ecs.config

  # Set up necessary rules to enable IAM roles for tasks
  sysctl -w net.ipv4.conf.all.route_localnet=1
  iptables -t nat -A PREROUTING -p tcp -d 169.254.170.2 --dport 80 -j DNAT --to-destination 127.0.0.1:51679
  iptables -t nat -A OUTPUT -d 169.254.170.2 -p tcp -m tcp --dport 80 -j REDIRECT --to-ports 51679

  # Run the agent
  docker run --name ecs-agent \
    --detach=true \
    --restart=always -d \
    --volume=/var/run/docker.sock:/var/run/docker.sock \
    --volume=/var/log/ecs:/log \
    --volume=/var/lib/ecs/data:/data \
    --net=host \
    --env-file=/etc/ecs/ecs.config \
    --env=ECS_LOGFILE=/log/ecs-agent.log \
    --env=ECS_DATADIR=/data/ \
    --env=ECS_ENABLE_TASK_IAM_ROLE=true \
    --env=ECS_ENABLE_TASK_IAM_ROLE_NETWORK_HOST=true \
    cloudstax/firecamp-amazon-ecs-agent:latest

  sleep 6

  # install firecamp docker volume plugin
  mkdir -p /var/log/firecamp
  docker plugin install --grant-all-permissions cloudstax/firecamp-volume:$version

  # TODO enable log driver after upgrade to 17.05/06
  # install firecamp docker log driver
  # docker plugin install cloudstax/firecamp-logs
fi

if [ "$containerPlatform" = "swarm" ]; then
  # Set the availability zone label to engine for deploying a service to the expected zone.
  # Another option is to use swarm node label. While, the node label could only be added on the manager node.
  # Would have to talk with the manager service to add label. Sounds complex. Simply use engine label.

  # get node's local az
  localAZ=$(curl http://169.254.169.254/latest/meta-data/placement/availability-zone)

  # parse azs to array
  OIFS=$IFS
  IFS=','
  read -a zones <<< "${azs}"
  IFS=$OIFS

  # add label for 1 and 2 availability zones
  # TODO support max 3 zones now.
  labels="--label $localAZ=true"
  for zone in "${zones[@]}"
  do
    if [ "$zone" = "$localAZ" ]; then
      continue
    fi
    if [ "$zone" \< "$localAZ" ]; then
      labels+=" --label $zone.$localAZ=true"
    else
      labels+=" --label $localAZ.$zone=true"
    fi
  done

  # set ulimit and labels
  sed -i "s/OPTIONS=\"--default-ulimit.*/OPTIONS=\"--default-ulimit nofile=100000:100000 --default-ulimit nproc=64000:64000 $labels\"/g" /etc/sysconfig/docker

  service docker start

  # get swarminit command to init swarm
  for i in `seq 1 3`
  do
    wget -O /tmp/firecamp-swarminit.tgz https://s3.amazonaws.com/cloudstax/firecamp/packages/$version/firecamp-swarminit.tgz
    if [ "$?" = "0" ]; then
      break
    elif [ "$i" = "3" ]; then
      echo "failed to get https://s3.amazonaws.com/cloudstax/firecamp/packages/$version/firecamp-swarminit.tgz"
      exit 2
    else
      # wget fail, sleep and retry
      sleep 3
    fi
  done

  tar -zxf /tmp/firecamp-swarminit.tgz -C /tmp
  chmod +x /tmp/firecamp-swarminit

  # initialize the swarm node
  /tmp/firecamp-swarminit -cluster="$clusterName" -role="$containerPlatformRole" -availability-zones="$azs"
  if [ "$?" != "0" ]; then
    echo "firecamp-swarminit error"
    exit 3
  fi

  if [ "$containerPlatformRole" = "worker" ]; then
    # worker node, install the firecamp plugin
    # install firecamp docker volume plugin
    mkdir -p /var/log/firecamp
    docker plugin install --grant-all-permissions cloudstax/firecamp-volume:$version PLATFORM="swarm" CLUSTER="$clusterName"

    # TODO enable log driver after upgrade to 17.05/06
    # install firecamp docker log driver
    # docker plugin install cloudstax/firecamp-logs
  fi
fi


