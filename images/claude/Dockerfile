FROM alpine:3.22

# busybox init reads /etc/inittab and hands off to openrc. Firecracker has
# one console, ttyS0, and no tty1-6.
RUN apk add --no-cache openrc openssh bash curl git ca-certificates libgcc libstdc++ ripgrep \
 && sed -i -e 's/^tty[0-9]/#&/' -e 's/^#ttyS0/ttyS0/' /etc/inittab \
 && echo ttyS0 >> /etc/securetty \
 && rc-update add devfs sysinit \
 && rc-update add procfs boot \
 && rc-update add sysfs boot \
 && rc-update add sshd default \
 && ssh-keygen -A \
 && passwd -d root

RUN curl -fsSL https://claude.ai/install.sh | bash

# docker export keeps files only, so ENV lines would be lost.
COPY profile.sh /etc/profile.d/sparrow.sh
