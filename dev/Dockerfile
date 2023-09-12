FROM registry.access.redhat.com/ubi8/ubi:8.1

RUN curl -L -o cincinnati-graph-data.tar.gz https://api.openshift.com/api/upgrades_info/graph-data

RUN mkdir -p /var/lib/cincinnati-graph-data && tar xvzf cincinnati-graph-data.tar.gz -C /var/lib/cincinnati-graph-data/ --no-overwrite-dir --no-same-owner

RUN mkdir -p /var/lib/cincinnati-graph-data/signatures/sha256/beda83fb057e328d6f94f8415382350ca3ddf99bb9094e262184e0f127810ce0 && curl -L https://mirror.openshift.com/pub/openshift-v4/signatures/openshift/release/sha256=beda83fb057e328d6f94f8415382350ca3ddf99bb9094e262184e0f127810ce0/signature-1 >/var/lib/cincinnati-graph-data/signatures/sha256/beda83fb057e328d6f94f8415382350ca3ddf99bb9094e262184e0f127810ce0/signature-1 && echo 1.2.0 >/var/lib/cincinnati-graph-data/version

CMD ["/bin/bash", "-c" ,"exec cp -rpv /var/lib/cincinnati-graph-data/* /var/lib/cincinnati/graph-data"]
