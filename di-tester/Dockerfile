From quay.io/netsys/di
Maintainer Ethan J. Jackson

RUN apk add --no-cache bash python py-pip curl openssh-client && \
pip install awscli
Copy bin/* /bin/
Copy config/config.spec /config/
Copy tests /tests
Copy config/id_rsa /root/.ssh/id_rsa
RUN chmod 0600 /root/.ssh/id_rsa

Entrypoint ["di-tester"]
