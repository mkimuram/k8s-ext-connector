#! /bin/bash

if [ -z "$EXTERNAL_SERVICE_NAME" ];then
  echo "EXTERNAL_SERVICE_NAME is required to be passed as an environment variable." >&2
  exit 1
fi
if [ -z "$DATA_FILE" ];then
  echo "DATA_FILE is required to be passed as an environment variable." >&2
  exit 1
fi

external_service_name=$EXTERNAL_SERVICE_NAME
data_file=$DATA_FILE

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

function delete_ssh_tunnel() {
  existing_ssh_tunnels=$(ps -C ssh -o pid,args --no-headers | awk -F ' ' '/-g -f -N -L/{print $1","$(NF-1)","$NF}')
  expected_ssh_tunnels=$(get_data "$data_file" "forwarder" "$external_service_name" "ssh-tunnel")

  while IFS= read -r existing;do
    existing_tun=$(echo "$existing" | awk -F ',' 'NF==3{print $2","$3}') 
    if [ -z "$existing_tun" ];then
      continue
    fi
    echo "$expected_ssh_tunnels" | grep -q "^$existing_tun\$"
    if [ $? -ne 0 ];then
      existing_pid=$(echo "$existing" | awk -F ',' 'NF==3{print $1}')
      run_command kill $existing_pid
    fi 
  done < <(printf '%s\n' "$existing_ssh_tunnels")
}

function delete_remote_ssh_tunnel() {
  existing_remote_ssh_tunnels=$(ps -C ssh -o pid,args --no-headers | awk -F ' ' '/-f -N -R/{print $1","$(NF-1)","$NF}')
  expected_remote_ssh_tunnels=$(get_data "$data_file" "forwarder" "$external_service_name" "remote-ssh-tunnel")

  while IFS= read -r existing_remote;do
    existing_remote_tun=$(echo "$existing_remote" | awk -F ',' 'NF==3{print $2","$3}') 
    if [ -z "$existing_remote_tun" ];then
      continue
    fi
    echo "$expected_remote_ssh_tunnels" | grep -q "^$existing_remote_tun\$"
    if [ $? -ne 0 ];then
      existing_pid=$(echo "$existing_remote" | awk -F ',' 'NF==3{print $1}')
      run_command kill $existing_pid
    fi 
  done < <(printf '%s\n' "$existing_remote_ssh_tunnels")
}

function create_ssh_tunnel() {
  existing_ssh_tunnels=$(ps -C ssh -o pid,args --no-headers | awk -F ' ' '/-g -f -N -L/{print $1","$(NF-1)","$NF}')
  expected_ssh_tunnels=$(get_data "$data_file" "forwarder" "$external_service_name" "ssh-tunnel")

  while IFS= read -r expected;do
    if [ -z "$expected" ];then
      continue
    fi
    echo "$existing_ssh_tunnels" | grep -q $expected 
    if [ $? -ne 0 ];then
      run_command ssh -o StrictHostKeyChecking=no -i /etc/ssh-key/id_rsa -g -f -N -L $(echo $expected | awk -F ',' '{print $1}') $(echo $expected | awk -F ',' '{print $2}')
    fi  
  done < <(printf '%s\n' "$expected_ssh_tunnels")
}

function create_remote_ssh_tunnel() {
  existing_remote_ssh_tunnels=$(ps -C ssh -o pid,args --no-headers | awk -F ' ' '/-f -N -R/{print $1","$(NF-1)","$NF}')
  expected_remote_ssh_tunnels=$(get_data "$data_file" "forwarder" "$external_service_name" "remote-ssh-tunnel")

  while IFS= read -r expected_remote;do
    if [ -z "$expected_remote" ];then
      continue
    fi
    echo "$existing_remote_ssh_tunnels" | grep -q $expected_remote
    if [ $? -ne 0 ];then
      run_command ssh -o StrictHostKeyChecking=no -i /etc/ssh-key/id_rsa -f -N -R $(echo $expected_remote | awk -F ',' '{print $1}') $(echo $expected_remote | awk -F ',' '{print $2}')
    fi  
  done < <(printf '%s\n' "$expected_remote_ssh_tunnels")
}

function update_iptables_rule() {
  expected_iptables_rules=$(get_data "$data_file" "forwarder" "$external_service_name" "iptables-rule")
  run_command iptables -t nat -F

  while IFS= read -r rule;do
    if [ -z "$rule" ];then
      continue
    fi
    run_command iptables -A $rule
  done < <(printf '%s\n' "$expected_iptables_rules")
}

function update() {
  delete_ssh_tunnel
  delete_remote_ssh_tunnel
  create_ssh_tunnel
  create_remote_ssh_tunnel
  update_iptables_rule
}

while :;do
  update
  sleep 10
done
