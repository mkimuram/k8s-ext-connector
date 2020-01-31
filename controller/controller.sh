#! /bin/bash 

INTERVAL=10
FWD_IMG="forwarder:0.1"
FWD_RULE_TMP="/tmp/genrules-rules.XXXXXX"
CONFIGMAP_NAME="external-service-data"
CONFIGMAP_KEY="data.yaml"
SECRET_NAME="my-ssh-key"
SECRET_KEY="id_rsa"
FORWARDER_NAMESPACE="external-services"
CONFIG_PATH_ON_GATEWAY_NODE="/tmp/rules.yaml"

if [ -z "${SSH_KEY_PATH}" ];then
  echo "SSH_KEY_PATH is required to be passed as an environment variable." >&2
  exit 1
fi

if [ -z "${GATEWAY_NODE_IP}" ];then
  echo "GATEWAY_NODE_IP is required to be passed as an environment variable." >&2
  exit 1
fi

function reconcile_forward_rules() {
  tmpfile=$(mktemp "${FWD_RULE_TMP}")
  # Generate new rules
  ./genrule.py "${CONF_DIR}" > "${tmpfile}"

  # Check if configmap has already exists
  if ( kubectl get configmap "${CONFIGMAP_NAME}" -n "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then
    # Check if existing configmap has the same contents to new one
    kubectl get configmap "${CONFIGMAP_NAME}" -n "${FORWARDER_NAMESPACE}" -o yaml \
    | python -c 'import sys,yaml; y=yaml.safe_load(sys.stdin.read()); sys.stdout.write(y["data"]["'${CONFIGMAP_KEY}'"] if "data" in y and "'${CONFIGMAP_KEY}'" in y["data"] else "")' \
    | diff -urN - "${tmpfile}"
    if [ $? -ne 0 ];then
      # Replace existing configmap
      kubectl create configmap "${CONFIGMAP_NAME}" -n "${FORWARDER_NAMESPACE}" --from-file="${CONFIGMAP_KEY}"="${tmpfile}" -o yaml --dry-run | kubectl replace -f -
      # Send to gateway node
      # TODO: imporve a way to share the config with gateway node
      scp -i "${SSH_KEY_PATH}" "${tmpfile}" "${GATEWAY_NODE_IP}:${CONFIG_PATH_ON_GATEWAY_NODE}"
    fi
  else
    # Create new configmap
    kubectl create configmap "${CONFIGMAP_NAME}" -n "${FORWARDER_NAMESPACE}" --from-file="${CONFIGMAP_KEY}"="${tmpfile}" -o yaml --dry-run | kubectl apply -f -
  fi 
  rm -f "${tmpfile}"
}

function create_fwd_pod() {
  local name=$1

  cat << EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  namespace: ${FORWARDER_NAMESPACE}
  name: ${name}
  labels:
    externalService: ${name}
spec:
  containers:
  - image: ${FWD_IMG}
    name: forwarder
    securityContext:
       privileged: true
    env:
    - name: EXTERNAL_SERVICE_NAME
      value: "${name}"
    - name: DATA_FILE
      value: "/etc/external-service/config/${CONFIGMAP_KEY}"
    volumeMounts:
    - name: data-file
      mountPath: "/etc/external-service/config"
    - name: ssh-key-volume
      mountPath: "/etc/ssh-key"
  volumes:
  - name: data-file
    configMap:
      name: ${CONFIGMAP_NAME}
  - name: ssh-key-volume
    secret:
      secretName: ${SECRET_NAME}
      defaultMode: 256
EOF
}

function output_ports() {
  local ports=$1

  while IFS= read -r line;do
    if [ -z "${line}" ];then
      continue
    fi
    echo "  ${line}"
  done < <(printf '%s\n' "${ports}")
}

function create_fwd_svc() {
  local name=$1
  local ports=$2
  cat << EOF | kubectl create -f -
apiVersion: v1
kind: Service
metadata:
  namespace: ${FORWARDER_NAMESPACE}
  name: ${name}
spec:
  selector:
    externalService: ${name}
$(output_ports "${ports}")
EOF
}

function reconcile_forwarder() {
  # TODO: Delete unnecessary pods and svcs, first.
  
  for file in $(find "${CONF_DIR}" -name "*yaml");do
    name=$(cat "${file}" | python -c 'import sys,yaml;y=yaml.safe_load(sys.stdin.read());print(y["metadata"]["name"] if "metadata" in y and "name" in y["metadata"] else "")')
    ports=$(cat "${file}" | python -c 'import sys,yaml;y=yaml.safe_load(sys.stdin.read());data={};data["ports"]=y["spec"]["ports"] if "spec" in y and "ports" in y["spec"] else ""; text=yaml.dump(data, indent=2, default_flow_style=False);print text')

    # Create fwd pod if not exists
    if ! ( kubectl get pod "${name}" -n "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then
      create_fwd_pod "${name}"
    fi

    # Create fwd svc if not exists
    if ! ( kubectl get svc "${name}" -n "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then
      create_fwd_svc "${name}" "${ports}"
    fi
  done
}

function init() {
  # Create namespace if not exists.
  if ! ( kubectl get ns "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then
    kubectl create ns "${FORWARDER_NAMESPACE}"
  fi

  # Create empty configmap if not exists. 
  if ! ( kubectl get configmap "${CONFIGMAP_NAME}" -n "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then
    tmpfile=$(mktemp "${FWD_RULE_TMP}")
    kubectl create configmap "${CONFIGMAP_NAME}" -n "${FORWARDER_NAMESPACE}" --from-file="${CONFIGMAP_KEY}"="${tmpfile}" -o yaml --dry-run | kubectl apply -f -
    rm -f "${tmpfile}"
  fi

  # Create secret if not exists. 
  if ! ( kubectl get secret "${SECRET_NAME}" -n "${FORWARDER_NAMESPACE}" >/dev/null 2>&1 );then
    kubectl create secret generic "${SECRET_NAME}" -n "${FORWARDER_NAMESPACE}" --from-file="${SECRET_KEY}"="${SSH_KEY_PATH}"
  fi
}

if [ $# -ne 1 ];then
  echo "Usage: $0 conf-dir"
  exit 1
fi

CONF_DIR=$1

init

while :;do
  reconcile_forwarder
  reconcile_forward_rules
  sleep "${INTERVAL}"
done
