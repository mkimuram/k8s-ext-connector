#! /bin/bash 

INTERVAL=10
VLAN_DEV_PREFIX="macvlan"
NS_PREFIX="ns"
DATA_FILE="/tmp/rules.yaml"

if [ -z "$NETMASK" ];then
  echo "NETMASK is required to be passed as an environment variable." >&2
  exit 1
fi

if [ -z "$DEFAULT_GATEWAY" ];then
  echo "DEFAULT_GATEWAY is required to be passed as an environment variable." >&2
  exit 1
fi

if [ -z "$NIC" ];then
  echo "NIC is required to be passed as an environment variable." >&2
  exit 1
fi

function get_data() {
  local file=$1
  local cname=$2
  local esname=$3
  local fname=$4

  # Get below data from yaml file whose name is {file}
  # {cname}:
  #   {esname}:
  #     {fname}:
  cat $file | python -c '
import yaml, sys
try:
  y=yaml.safe_load(sys.stdin.read())
  if "'$cname'" in y and "'$esname'" in y["'$cname'"] and "'$fname'" in y["'$cname'"]["'$esname'"]:
    print(y["'$cname'"]["'$esname'"]["'$fname'"])
except:
  print ""
'
}

function run_command() {
  if [ "$DEBUG" != true ];then
    $@
  else
    echo "$@"
  fi
}

function setup_ip() {
  local ip_address=$1

  local hex_ip=$(echo "${ip_address}" | sed 's/\./ /g'  | xargs printf "%02x%02x%02x%02x")
  local vlan_dev="${VLAN_DEV_PREFIX}${hex_ip}"
  local ns="${NS_PREFIX}${hex_ip}"
  local pid_file=/run/sshd-${hex_ip}.pid

  # Create ns if not exists
  if ! ( ip netns | grep -q "$ns" );then  
    ip netns add "${ns}"
  fi

  # Create macvlan device and add to ns if not exists in ns
  if ! ( ip netns exec "$ns" ip link show "${vlan_dev}" >/dev/null 2>&1 );then
    ip link add "${vlan_dev}" link "${NIC}" type macvlan mode bridge
    ip link set "${vlan_dev}" netns "${ns}"
  fi

  # Make lo and macvlan device up
  ip netns exec "${ns}" ip link set lo up
  ip netns exec "${ns}" ip link set "${vlan_dev}" up

  # Set ip address to macvlan device if not set 
  if ! ( ip netns exec "${ns}" ip -4 -o addr show "${vlan_dev}" | awk '{print $4}' | grep -q "${ip_address}"/"${NETMASK}" );then
    ip netns exec "${ns}" ip addr add "${ip_address}"/"${NETMASK}" dev "${vlan_dev}"
  fi 

  # Set default gateway to macvlan device if not set
  if ! ( ip netns exec "${ns}" ip route | grep "default" | grep "${vlan_dev}" | grep -q "${DEFAULT_GATEWAY}" );then
    ip netns exec "${ns}" ip route add default via "${DEFAULT_GATEWAY}"
  fi 

  # Run sshd for the namespace if not running
  if [ ! -f "${pid_file}" ];then
    ip netns exec "${ns}" /usr/sbin/sshd -o PidFile="${pid_file}"
  fi
}

function init() {
  cat ${CONF} | while read ip;do
    setup_ip ${ip}
  done
}

function apply_iptables_rules() {
  local ip_address=$1

  local hex_ip=$(echo "${ip_address}" | sed 's/\./ /g'  | xargs printf "%02x%02x%02x%02x")
  local ns="${NS_PREFIX}${hex_ip}"

  if [ ! -f "$DATA_FILE" ];then
    return
  fi

  expected_iptables_rules=$(get_data "$DATA_FILE" "gateway" "$ip_address" "iptables-rule")

  run_command ip netns exec "${ns}" iptables -t nat -F

  while IFS= read -r rule;do
    if [ -z "$rule" ];then
      continue
    fi
    run_command ip netns exec "${ns}" iptables -A $rule
  done < <(printf '%s\n' "$expected_iptables_rules")
}

function reconcile_iptables_rules() {
  cat "${CONF}" | while read ip;do
    apply_iptables_rules "${ip}"
  done
}

if [ $# -ne 1 ];then
  echo "Usage: $0 conf-file"
  exit 1
fi

CONF=$1

init

while :;do
  reconcile_iptables_rules
  sleep "${INTERVAL}"
done
