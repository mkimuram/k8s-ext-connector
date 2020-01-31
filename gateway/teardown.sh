#! /bin/bash 

NS_PREFIX="ns"

function teardown_ip() {
  local ip_address=$1

  local hex_ip=$(echo "${ip_address}" | sed 's/\./ /g'  | xargs printf "%02x%02x%02x%02x")
  local ns="${NS_PREFIX}${hex_ip}"
  local pid_file=/run/sshd-${hex_ip}.pid

  # Delete ns if exists
  if ( ip netns | grep -q "$ns" );then  
    ip netns del "${ns}"
  fi

  # kill sshd for the namespace if running
  if [ -f "${pid_file}" ];then
    kill $(cat "${pid_file}")
  fi
}

function teardown() {
  cat ${CONF} | while read ip;do
    teardown_ip ${ip}
  done
}

if [ $# -ne 1 ];then
  echo "Usage: $0 conf-file"
  exit 1
fi

CONF=$1

teardown
exit 0

