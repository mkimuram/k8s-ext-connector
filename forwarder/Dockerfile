FROM centos:7

RUN yum -y update && \
yum -y install iptables openssh-clients PyYAML && \
yum clean all

COPY forwarder.sh /
CMD /forwarder.sh
