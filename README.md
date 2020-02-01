# k8s-ext-connector
k8s-ext-connector connects k8s cluster and external servers in a way that their source IPs are static ones.
This project is still under development, so never use this version in production. 
Also, this is just a prototype to have further discussions on implementation ideas and APIs, and it will eventually be replaced with a k8s operator implementation.

## Background
- In k8s, source IP of egress traffic is not static. In some CNI implementation, it is translated (SNAT) to appear as the node IP when it leaves the cluster. However, there are many devices and software that use IP based ACLs to restrict incoming traffic for security reasons and bandwidth limitations. As a result, this kind of ACLs outside k8s cluster will block packets from the pod, which causes a connectivity issue. To resolve this issue, we need a feature to assign a particular static egress source IP to a pod.
- Existing servers and k8s cluster are not always in the same network, they can be in different clouds or even in on-premise. Therefore, there will be a case that pod can't access directly to existing servers and vice versa. It would be nice if user can allow connections between certain pods and certain existing servers with easy configurations.

## Use case
1. There is an existing database server which restricts access by source IP in on-premise data center. New application deployed on k8s in the same data center needs to access to the database server.
2. There is a service running in a VM instance on a cloud. New application deployed on k8s in a different cloud needs to access to the service.
3. There is an existing database server which restricts access by source IP in on-premise data center. New application deployed on k8s in a different cloud needs to access to the database server.

## How it works
See [connection from k8s to external server](https://github.com/kubernetes/enhancements/pull/1105#issuecomment-571694606) and [connection from external server to k8s](https://github.com/kubernetes/enhancements/pull/1105#issuecomment-575424609) for basic ideas. Scripts in this repo will automatically configure iptables rules and ssh tunnels ,which are explained in the URLs, by the [API](#API).

There are mainly three scripts:
- [forwarder.sh](forwarder/forwarder.sh): It runs inside forwarder pod, which is created per external server. It creates ssh tunnels and applys iptables rules for accessing to the external server,
- [gateway.sh](gateway/gateway.sh): It runs on the gateway node. It creates network namespace and assign external IP to it, and run sshd per IP. Then, it applys iptables rules for accessing from the external server,
- [controller.sh](controller/controller.sh): It creates and deletes forwarder pod and keep configurations for forwarder.sh and gateway.sh up-to-date.

For multi-cloud usecases, submariner should help achieve this goal, by connecting k8s clusters.

## Usage
To try this, you need 3 servers, external server, gateway server, and k8s server (Just for test, external server and k8s server can be the same). Also, these servers need to be acccessible each other, and you need to assign 2 extra IPs to gateway server (Actually, external server and k8s server doesn't necessary need to be accessible directly, but will be able to be accessible via gateay server).

1. On external server
    1. Run an application that will be accessed from pods on k8s cluster. Below command will run http server on port 8000. 

        ```console
        $ python -m SimpleHTTPServer 2>&1 | tee /tmp/access.log
        ```

2. On gateway server
    1. Clone k8s-ext-connector repo.

        ```console
        # git clone https://github.com/mkimuram/k8s-ext-connector.git
        # cd k8s-ext-connector/gateway
        ```

    2. Review ips.conf. Replace IPs to be used as external IPs. (External IPs here are the extra IPs that is mentioned above and will be assinged by the script below.)

        ```console
        # vi ips.conf
        ```

    3. Edit `/etc/ssh/sshd_config` and set `GatewayPorts` to `clientspecified`.

        ```console
        GatewayPorts clientspecified
        ```

    4. Run gateway.sh.

        ```console
        export NETMASK=24
        export DEFAULT_GATEWAY=192.168.122.1
        export NIC=eth0
        # ./gateway.sh ips.conf
        ```

3. On k8s server
    1. Setup k8s cluster if not exists and create pods and services.

        ```console
        # kind create cluster
        ```

        ```console
        # kubectl create ns ns1
        # kubectl run my-pod1 -n ns1 --image=centos:7 --restart=Never --command -- python -m SimpleHTTPServer 80
        # kubectl expose pod my-pod1 --name=my-service1 -n ns1 --port=80
        # kubectl create ns ns2
        # kubectl run my-pod2 -n ns2 --image=centos:7 --restart=Never --command -- python -m SimpleHTTPServer 80
        # kubectl expose pod my-pod2 --name=my-service2 -n ns2 --port=80
        ```

    2. Clone k8s-ext-connector repo.

        ```console
        # git clone https://github.com/mkimuram/k8s-ext-connector.git
        ```

    3. Build forwarder image.

        ```console
        # cd k8s-ext-connector/forwarder
        # docker build -t forwarder:0.1 .
        ```

    4. Make forwarder image available on k8s cluster (For a cluster created with kind, do below.)

        ```console
        # kind load docker-image forwarder:0.1
        ```

    5. Define gateway server's IP address as a bash environment variable (Replace the IP with proper one). 

        ```console
        # export GATEWAY_NODE_IP="192.168.122.192"
        ```

    6. Prepare ssh key to connect to gateway node. (Hit enter for all prompts in `ssh-keygen` and type "yes" and password for `ssh-copy-id`.)

        ```console
        # export SSH_KEY_PATH="/root/forwarder_ssh_key_rsa"
        # ssh-keygen -t rsa -f "${SSH_KEY_PATH}"
        # ssh-copy-id -i "${SSH_KEY_PATH}" "${GATEWAY_NODE_IP}" 
        ```

    7. Review conf files under conf/ directory. Replace `targetIP` and `sourceIP` to fit to your environment. (`targetIP` should be external server's IP and `sourceIP` should be external IPs above. See [API](#API) for details.)

        ```console
        # cd ../controller
        # vi conf/my-external-service1.yaml
        ```

    8. Run controller.sh

        ```console
        # ./controller.sh conf
        ```

4. Test connectivity
    1. Connect to external server from my-pod1 and check source IP of the access.
        - On k8s server

        ```console
        # kubectl exec -it my-pod1 -n ns1 -- curl my-external-service1.external-services.svc.cluster.local:8000
        ```

        - On external server

        ```console
        # tail /tmp/access.log
        ```

    2. Connect to external server from my-pod2 and check source IP of the access.
        - On k8s server

        ```console
        # kubectl exec -it my-pod2 -n ns2 -- curl my-external-service1.external-services.svc.cluster.local:8000
        ```

        - On external server

        ```console
        # tail /tmp/access.log
        ```

    3. Connect to my-pod1, or my-service1, from external server and check source IP of the access.
        - On external server

        ```console
        # EXTERNAL_IP1=192.168.122.200
        # curl "${EXTERNAL_IP1}"
        ```

        - On k8s server

        ```console
        # kubectl logs my-pod1 -n ns1 
        ```

    4. Connect to my-pod2, or my-service2, from external server and check source IP of the access.
        - On external server

        ```console
        # EXTERNAL_IP2=192.168.122.201
        # curl "${EXTERNAL_IP2}"
        ```

        - On k8s server

         ```console
        # kubectl logs my-pod2 -n ns2 
        ```

## Undeploy
1. On k8s server
    1. Stop `./controller.sh conf` process.
    2. Run teardown.sh in k8s-ext-connector/controller directory. Note that conf/ directory should contain the same contents that are passed to controller.sh.

        ```console
        # ./teardown.sh conf
        ```

2. On gateway server
    1. Stop `./gateway.sh ips.conf` process.
    2. Run teardown.sh in k8s-ext-connector/gateway directory. Note that ips.conf should have the same content that is passed to gateway.sh.

        ```console
        # ./teardown.sh ips.conf
        ```
3. On external server
    1. Stop `python -m SimpleHTTPServer` process.

## API
API is subject to change after further discussions. However, current API is like below:

```
apiVersion: externalservice.example.com/v1alpha1
kind: ExternalService
metadata:
  name: my-external-service1
spec:
  targetIP: 192.168.122.139
  sources:
    - service: 
        namespace: ns1
        name: my-service1
      sourceIP: 192.168.122.200
    - service: 
        namespace: ns2
        name: my-service2
      sourceIP: 192.168.122.201
  ports:
    - protocol: TCP
      port: 8000
      targetPort: 8000
```

This defines that:
  - Access to `targetPort` of service named `metadata.name` will be forwarded to `port` of `targetIP` if sources are the pods associated with the `service`,
  - The source IP of the packets from the pod associated with the `service` will be `sourceIP` defined for the `service`,
  - Access from `targetIP` to `service`'s port of `sourceIP` will be forwarded to the `service`.

In above case:
  - Acccess to `my-external-service1:8000` will be forwarded to `192.168.122.139:8000` if sources are the pods associated with `my-service1` or `my-service2`, 
  - The source IP of the packets from the pods associated with `my-service1` will be `192.168.122.200` and that with `my-service2` will be `192.168.122.201`,
  - Access from `192.168.122.139` to `192.168.122.200:80` will be forwarded to `my-service1:80` and that to `192.168.122.201:80` will be forwarded to `my-service2:80` (if both `my-service1` and `my-service2` define port 80).

## Limitations
- Only TCP is handled now and UDP is not handled. (Supporting UDP with ssh tunnel will be possible, technically.)
- Remote ssh tunnels are created for all cases, but it won't always be necessary. We might consider adding like `bidirectional` flag and avoid creating ones if it is set to false.
- Updating configurations and applying configurations to forwarder pods and the gateway node are done async on INTERVAL(10 seconds), which will make a big delay until the configurations are reflected. If implemented with k8s operator, this kind of delays will be reduced by updating and applying configurations when changes are detected. 
- It assumes that sshds are run by root user. We will need to apply principle of least privilege for better security. (Also, shell access for these ssh connection won't needed.)
