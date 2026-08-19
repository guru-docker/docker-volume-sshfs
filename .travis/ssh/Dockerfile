# Throwaway sshd server used only by the integration tests.
# Accepts both password auth (root:root) and the test key in this directory.
FROM alpine:3.21

RUN apk add --no-cache openssh-server \
	&& ssh-keygen -A \
	&& echo 'root:root' | chpasswd \
	&& sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config \
	&& sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config \
	&& mkdir -p /root/.ssh \
	&& chmod 700 /root/.ssh

COPY id_rsa.pub /root/.ssh/authorized_keys
RUN chmod 600 /root/.ssh/authorized_keys

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
