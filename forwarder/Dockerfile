FROM centos:7

RUN yum -y update && \
yum -y install iptables openssh-clients iproute tcpdump && \
yum clean all

COPY forwarder /
CMD /forwarder
