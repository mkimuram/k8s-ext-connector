#! /bin/bash

deploy_dir="deploy"
files=$(cat << EOF
service_account.yaml
role.yaml
role_binding.yaml
crds/submariner.io_externalservices_crd.yaml
crds/submariner.io_forwarders_crd.yaml
crds/submariner.io_gateways_crd.yaml
operator.yaml
EOF
)

if [ "$1" != "-u" ];then
	for file in ${files};do
		kubectl create -f ${deploy_dir}/${file}
	done
else
	echo "${files}" | tac - | while read file;do
		kubectl delete -f ${deploy_dir}/${file}
	done
fi
