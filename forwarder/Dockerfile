FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

RUN microdnf install -y iptables iproute        && \
    microdnf update -y && rm -rf /var/cache/yum && \
	microdnf clean all

COPY bin/forwarder /
CMD /forwarder
