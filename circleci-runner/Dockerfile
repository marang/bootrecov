FROM ubuntu:22.04

RUN apt-get update && \
    apt-get install -y curl sudo qemu-system-x86 kmod && \
    useradd -m circleci && \
    echo "circleci ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers

USER circleci
WORKDIR /home/circleci

# Install CircleCI runner
RUN curl -fsSL https://circle.ci/runner-install.sh | bash

COPY launch-agent-config.yaml .

CMD ["/home/circleci/circleci-launch-agent"]
