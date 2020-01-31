#! /bin/bash

CONFIGMAP_NAME="external-service-data"
SECRET_NAME="my-ssh-key"
FORWARDER_NAMESPACE="external-services"

if [ $# -ne 1 ];then
  echo "Usage: $0 conf-dir"
  exit 1
fi

CONF_DIR=$1

function delete_forward_rules() {
  if ( kubectl get configmap "${CONFIGMAP_NAME}" -n "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then  
    kubectl delete configmap ${CONFIGMAP_NAME} -n "${FORWARDER_NAMESPACE}"
  fi
}

function delete_forwarder() {
  for file in $(find "${CONF_DIR}" -name "*yaml");do
    name=$(cat "${file}" | python -c 'import sys,yaml;y=yaml.safe_load(sys.stdin.read());print(y["metadata"]["name"] if "metadata" in y and "name" in y["metadata"] else "")')
    ports=$(cat "${file}" | python -c 'import sys,yaml;y=yaml.safe_load(sys.stdin.read());data={};data["ports"]=y["spec"]["ports"] if "spec" in y and "ports" in y["spec"] else ""; text=yaml.dump(data, indent=2, default_flow_style=False);print text')

    # Delete fwd pod if not exists
    if ( kubectl get pod "${name}" -n "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then
      kubectl delete pod "${name}" -n "${FORWARDER_NAMESPACE}"
    fi

    # Delete fwd svc if not exists
    if ( kubectl get svc "${name}" -n "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then
      kubectl delete svc "${name}" -n "${FORWARDER_NAMESPACE}"
    fi
  done
}

function delete_secret() {
  if ( kubectl get secret "${SECRET_NAME}" -n "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then  
    kubectl delete secret ${SECRET_NAME} -n "${FORWARDER_NAMESPACE}"
  fi
}

function delete_namespace() {
  if ( kubectl get ns "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then
    kubectl delete ns "${FORWARDER_NAMESPACE}"
  fi
}

delete_forwarder
delete_forward_rules
delete_secret
delete_namespace
