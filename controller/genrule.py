#! /usr/bin/python

import glob, sys, subprocess, yaml

MIN_PORT_NUM = 2049 
MAX_PORT_NUM = 65536
FORWARDER_NAMESPACE = 'external-services'

def to_namespaced_name(namespace, name):
  return str(namespace) + ':' + str(name)

def to_namespace(namespaced_name):
  return namespaced_name.split(':')[0]

def to_name(namespaced_name):
  return namespaced_name.split(':')[1]

# get_podip returns pod IP for the pod whose name is {name}.
def get_podip(namespace, name):
  args = ['kubectl', 'get', 'pod', name, '-n', namespace, '-o', 'custom-columns=podIP:status.podIP', '--no-headers']

  try:
    res = subprocess.check_output(args)
    return str(res).rstrip()
  except:
    return ""

# get_clusterip returns cluster IP for the service whose name is {name}.
def get_clusterip(service):
  namespace = to_namespace(service)
  name = to_name(service)
  args = ['kubectl', 'get', 'svc', name, '-n', namespace, '-o', 'custom-columns=clusterIP:spec.clusterIP', '--no-headers']

  try:
    res = subprocess.check_output(args)
    return str(res).rstrip()
  except:
    return ""

# get_svc_ports returns an array of service ports for the service whose name is {name}.
def get_svc_ports(service):
  svc_ports = []
  namespace = to_namespace(service)
  name = to_name(service)
  args = ['kubectl', 'get', 'svc', name, '-n', namespace, '-o', 'yaml']

  try:
    res = subprocess.check_output(args)
    y=yaml.safe_load(res)
    for s in y['spec']:
      if s == 'ports':
        for port in y['spec']['ports']:
          if 'port' in port:
           svc_ports.append(port['port'])
    return svc_ports
  except:
    return svc_ports

# get_endpointips returns an array of endpoint IPs for the endpoint whose name is {name}.
def get_endpointips(service):
  endpointips = []
  namespace = to_namespace(service)
  name = to_name(service)
  args = ['kubectl', 'get', 'endpoints', name, '-n', namespace, '-o', 'yaml']

  try:
    res = subprocess.check_output(args)
    y=yaml.safe_load(res)
    for s in y["subsets"]:
      if "addresses" in s:
        for addr in s["addresses"]:
          if "ip" in addr:
            endpointips.append(addr["ip"])
    return endpointips
  except:
    return endpointips

# gen_port generates and returns a port that is not used for ssh tunnel and target_port.
def gen_port(source_ip, target_port, used_ports):
  for port in range(MIN_PORT_NUM, MAX_PORT_NUM):
    if port not in used_ports:
      used_ports[port] = source_ip + ":" + str(target_port)
      return port
  return ""

# get_port returns a port for a combination of source_ip and target_port, which is generated in gen_port.
def get_port(source_ip, target_port, used_ports):
  for port in used_ports:
    if used_ports[port] == source_ip + ":" + str(target_port):
        return port
  return ""

# gen_remote_port generates and returns a port that is not used for remote ssh tunnel and target_port.
def gen_remote_port(source_ip, cluster_ip, svc_port, used_remote_ports):
  if source_ip not in used_remote_ports:
    used_remote_ports[source_ip] = {}

  for port in range(MIN_PORT_NUM, MAX_PORT_NUM):
    if port not in used_remote_ports[source_ip]:
      used_remote_ports[source_ip][port] = cluster_ip + ":" + str(svc_port)
      return port
  return ""

# get_remote_port returns a port for a combination of target_ip, cluster_ip, and svc_port, which is generated in gen_remote_port.
def get_remote_port(source_ip, cluster_ip, svc_port, used_remote_ports):
  if source_ip not in used_remote_ports:
    return ""

  for port in used_remote_ports[source_ip]:
    if used_remote_ports[source_ip][port] == cluster_ip + ":" + str(svc_port):
      return port
  return ""

# mark_ports_used marks target_ports as used to avoid these ports being used by gen_port. 
def mark_ports_used(target_ports, used_ports):
  for target_port in target_ports:
    used_ports[target_port] = '-'

# mark_remote_ports_used marks ports as used to avoid these ports being used by gen_remote_port.
def mark_remote_ports_used(source_ips, sourceip_service_maps, used_remote_ports):
  for source_ip in source_ips:
    if source_ip not in used_remote_ports:
      used_remote_ports[source_ip] = {}
    for svc in sourceip_service_maps[source_ip]:
      for svc_port in get_svc_ports(svc):
        used_remote_ports[source_ip][svc_port] = '-'

# load_yaml loads yaml file and returns yaml data.
def load_yaml(file):
  with open(file) as f:
    return yaml.safe_load(f)

# get_name returns name as string from an ExternalService yaml.
def get_name(y):
  if "metadata" in y and "name" in y["metadata"]:
    return y["metadata"]["name"]
  else:
    return ""

# get_targetip returns target IP as string from an ExternalService yaml.
def get_targetip(y):
  if "spec" in y and "targetIP" in y["spec"]:
    return y["spec"]["targetIP"]
  else:
    return ""

# get_sourceips creates and returns an array of source IPs from an ExternalService yaml.
def get_sourceips(y):
  sourceIPs = []
  if "spec" in y and "sources" in y["spec"]:
    for source in y["spec"]["sources"]:
      if "sourceIP" in source:
        sourceIPs.append(source["sourceIP"])
  return sourceIPs
   
# get_targetports creates and returns an array of target ports from an ExternalService yaml.
def get_targetports(y):
  targetPorts = []
  if "spec" in y and "ports" in y["spec"]:
    for port in y["spec"]["ports"]:
      if "targetPort" in port:
        targetPorts.append(port["targetPort"])
  return targetPorts

# get_sourceip_service_maps creates and returns a dictionary whose key is source IP and value is dictionary of services from an ExternalService yaml.
def get_sourceip_service_maps(y):
  sourceip_service_maps = {}
  if "spec" in y and "sources" in y["spec"]:
    for source in y["spec"]["sources"]:
      if "sourceIP" in source and "service" in source:
        if "namespace" in source["service"] and "name" in source["service"]:
          namespace = source["service"]["namespace"]
          name = source["service"]["name"]
          namespaced_name = to_namespaced_name(namespace, name)
        else:
          continue
        if not source["sourceIP"] in sourceip_service_maps:
          svcmap = {namespaced_name} 
          sourceip_service_maps[source["sourceIP"]] = svcmap
        else:
          svcmap = sourceip_service_maps[source["sourceIP"]]
          svcmap.append(namespaced_name)
  return sourceip_service_maps

# gen_ssh_tunnel_rules generates ssh tunnel rules.
def gen_ssh_tunnel_rules(target_ip, source_ips, target_ports, used_ports):
  rules = ""
  for source_ip in source_ips:
    for target_port in target_ports:
      fwd_port = gen_port(source_ip, target_port, used_ports)
      # Skip generating rules if any of values are not available
      if fwd_port == "" or target_ip == "" or target_port == "" or source_ip == "":
        continue
      rules += "{}:{}:{},{}\n".format(fwd_port, target_ip, target_port, source_ip)
  return rules

# gen_remote_ssh_tunnel_rules generates remote ssh tunnel rules.
def gen_remote_ssh_tunnel_rules(target_ip, source_ips, sourceip_service_maps, used_remote_ports):
  rules = ""
  for source_ip in source_ips:
    for svc in sourceip_service_maps[source_ip]: 
      cluster_ip = get_clusterip(svc)
      for svc_port in get_svc_ports(svc):
        remote_fwd_port = gen_remote_port(source_ip, cluster_ip, svc_port, used_remote_ports)
        # Skip generating rules if any of values are not available
        if source_ip == "" or remote_fwd_port == "" or cluster_ip == "" or svc_port == "":
          continue
        rules += "{}:{}:{}:{},{}\n".format(source_ip, remote_fwd_port, cluster_ip, svc_port, source_ip)
  return rules

# gen_iptables_rules generates iptables rules.
def gen_iptables_rules(target_ip, target_ports, fwdpod_ip, sourceip_service_maps, used_ports):
  rules = ""
  for source_ip in sourceip_service_maps:
    for target_port in target_ports:
      fwd_port = get_port(source_ip, target_port, used_ports)
      for svc in sourceip_service_maps[source_ip]: 
        for sip in get_endpointips(svc):
          # Skip generating rules if any of values are not available
          if fwdpod_ip == "" or sip == "" or target_port == "" or fwd_port == "" or target_ip == "":
            continue
          # Add rules
          rules += "PREROUTING -t nat -m tcp -p tcp --dst {} --src {} --dport {} -j DNAT --to-destination {}:{}\n".format(fwdpod_ip, sip, target_port, fwdpod_ip, fwd_port)
          rules += "POSTROUTING -t nat -m tcp -p tcp --dst {} --dport {} -j SNAT --to-source {}\n".format(target_ip, fwd_port, fwdpod_ip)
  return rules

def gen_gateway_iptables_rules(target_ip, source_ip, sourceip_service_maps, used_remote_ports):
  rules = ""
  for svc in sourceip_service_maps[source_ip]:
    for svc_port in get_svc_ports(svc):
      cluster_ip = get_clusterip(svc)
      remote_fwd_port = get_remote_port(source_ip, cluster_ip, svc_port, used_remote_ports)
      if source_ip == "" or target_ip == "" or svc_port == "" or remote_fwd_port == "":
        continue
      # Add rules
      rules += "PREROUTING -t nat -m tcp -p tcp --dst {} --src {} --dport {} -j DNAT --to-destination {}:{}\n".format(source_ip, target_ip, svc_port, source_ip, remote_fwd_port)
      rules += "POSTROUTING -t nat -m tcp -p tcp --dst {} --dport {} -j SNAT --to-source {}\n".format(target_ip, remote_fwd_port, source_ip)
  return rules


def add_rules(y, rules, used_remote_ports):
  used_ports = {}

  # Parse data from yaml file.
  name = get_name(y)
  target_ip = get_targetip(y)
  source_ips = get_sourceips(y)
  target_ports = get_targetports(y)
  sourceip_service_maps = get_sourceip_service_maps(y)
  fwdpod_ip = get_podip(FORWARDER_NAMESPACE, name)

  # Set ports that are used by service as used.
  mark_ports_used(target_ports, used_ports)
  mark_remote_ports_used(source_ips, sourceip_service_maps, used_remote_ports)

  # Generate rules for forwarder, or rules for ssh tunnel, remote ssh tunnel, and iptables. 
  rules['forwarder'][name] = {}
  rules['forwarder'][name]['ssh-tunnel'] = gen_ssh_tunnel_rules(target_ip, source_ips, target_ports, used_ports)
  rules['forwarder'][name]['remote-ssh-tunnel'] = gen_remote_ssh_tunnel_rules(target_ip, source_ips, sourceip_service_maps, used_remote_ports)
  rules['forwarder'][name]['iptables-rule'] = gen_iptables_rules(target_ip, target_ports, fwdpod_ip, sourceip_service_maps, used_ports)

  # Generate rules for gateway, or rules for gateway iptables. 
  for source_ip in source_ips:
    if source_ip not in rules['gateway']:
      rules['gateway'][source_ip] = {'iptables-rule': ""} 
    rules['gateway'][source_ip]['iptables-rule'] += gen_gateway_iptables_rules(target_ip, source_ip, sourceip_service_maps, used_remote_ports)

def gen_all_rules(dir):
  rules = {'forwarder': {}, 'gateway': {}}
  used_remote_ports = {}
 
  # Load each yaml file and generate rules for the file .
  for file in glob.glob(dir+"/*.yaml"):
    y = load_yaml(file)
    add_rules(y, rules, used_remote_ports)

  return rules

def main():
  argv = sys.argv
  argc = len(argv)
  if (argc != 2):
    print("Usage: {} conf-dir").format(argv[0])
    exit(1)

  dir = str(argv[1])
  
  rules = gen_all_rules(dir)
  print(yaml.dump(rules, indent=2, default_flow_style=False, default_style='|'))

if __name__=="__main__":
  main()

