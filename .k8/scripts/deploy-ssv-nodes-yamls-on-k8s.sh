#!/bin/bash

set -x

if [[ -z $1 ]]; then
  echo "Please provide DOCKERREPO"
  exit 1
fi

if [[ -z $2 ]]; then
  echo "Please provide IMAGETAG"
  exit 1
fi

if [[ -z $3 ]]; then
  echo "Please provide NAMESPACE"
  exit 1
fi

if [[ -z $4 ]]; then
  echo "Please provide number of replicas"
  exit 1
fi

if [[ -z $5 ]]; then
  echo "Please provide deployment type: blox-infra-stage|blox-infra-prod"
  exit 1
fi

if [[ -z $6 ]]; then 
  echo "Please provide k8s context"
  exit 1
fi

if [[ -z $7 ]]; then
  echo "Pleae provide domain suffix"
  exit 1
fi

if [[ -z ${8} ]]; then
  echo "Please provide k8s app version"
  exit 1
fi

if [[ -z $9 ]]; then
  echo "Please provide health check image"
  exit 1
fi

if [[ -z ${10} ]]; then
  echo "Please provide nodes cpu limit"
  exit 1
fi

if [[ -z ${11} ]]; then
  echo "Please provide nodes mem limit"
  exit 1
fi


DOCKERREPO=$1
IMAGETAG=$2
NAMESPACE=$3
REPLICAS=$4
DEPL_TYPE=$5
K8S_CONTEXT=$6
DOMAIN_SUFFIX=$7
K8S_API_VERSION=$8
HEALTH_CHECK_IMAGE=$9
NODES_CPU_LIMIT=${10}
NODES_MEM_LIMIT=${11}


echo $DOCKERREPO
echo $IMAGETAG
echo $NAMESPACE
echo $REPLICAS
echo $DEPL_TYPE
echo $K8S_CONTEXT
echo $DOMAIN_SUFFIX
echo $K8S_API_VERSION
echo $HEALTH_CHECK_IMAGE
echo $NODES_CPU_LIMIT
echo $NODES_MEM_LIMIT

# create namespace if not exists
if ! kubectl --context=$K8S_CONTEXT get ns | grep -q $NAMESPACE; then
  echo "$NAMESPACE created"
  kubectl --context=$K8S_CONTEXT create namespace $NAMESPACE
fi

#config
#if [[ -d .k8/configmaps/ ]]; then
#config
  #for file in $(ls -A1 .k8/configmaps/); do
    #sed -i -e "s|REPLACE_NAMESPACE|${NAMESPACE}|g" ".k8/configmaps/${file}" 
  #done
#fi

#if [[ -d .k8/secrets/ ]]; then
  #for file in $(ls -A1 .k8/secrets/); do
   #sed -i -e "s|REPLACE_NAMESPACE|${NAMESPACE}|g" ".k8/secrets/${file}"
  #done
#fi

if [[ -d .k8/yamls/ ]]; then
  for file in $(ls -A1 .k8/yamls/); do
   sed -i -e "s|REPLACE_NAMESPACE|${NAMESPACE}|g" \
          -e "s|REPLACE_DOCKER_REPO|${DOCKERREPO}|g" \
          -e "s|REPLACE_REPLICAS|${REPLICAS}|g" \
          -e "s|REPLACE_DOMAIN_SUFFIX|${DOMAIN_SUFFIX}|g" \
          -e "s|REPLACE_API_VERSION|${K8S_API_VERSION}|g" \
          -e "s|REPLACE_HEALTH_IMAGE|${HEALTH_CHECK_IMAGE}|g" \
          -e "s|REPLACE_NODES_CPU_LIMIT|${NODES_CPU_LIMIT}|g" \
          -e "s|REPLACE_NODES_MEM_LIMIT|${NODES_MEM_LIMIT}|g" \
	  -e "s|REPLACE_IMAGETAG|${IMAGETAG}|g" ".k8/yamls/${file}" || exit 1
  done
fi

#disable automounting of tokens
#kubectl --context=admin-prod patch serviceaccount default -p "automountServiceAccountToken: false" -n ${NAMESPACE}

#apply network policy
#for file in $(ls -A1 .k8/network-policy/); do
#  sed -i -e "s|REPLACE_NAMESPACE|${NAMESPACE}|g" .k8/network-policy/${file} || exit 1
#done


#secure namespace
#if [ "${DEPL_TYPE}" = "prod" ]; then



  #kubectl --context=admin-prod apply -f .k8/psp/ -n ${NAMESPACE} || exit 1

  #apply network policy
  #for file in $(ls -A1 .k8/network-policy/); do
    #sed -i -e "s|REPLACE_NAMESPACE|${NAMESPACE}|g" .k8/network-policy/${file} || exit 1
  #done
  #kubectl --context=admin-prod apply -f .k8/network-policy/ -n ${NAMESPACE} || exit 1


#fi

#deploy
kubectl --context=$K8S_CONTEXT apply -f .k8/yamls/ssv-node-v2-2-deployment.yml || exit 1
kubectl --context=$K8S_CONTEXT apply -f .k8/yamls/ssv-node-v2-3-deployment.yml || exit 1
kubectl --context=$K8S_CONTEXT apply -f .k8/yamls/ssv-node-v2-4-deployment.yml || exit 1
