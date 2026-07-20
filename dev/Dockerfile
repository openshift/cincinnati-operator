FROM registry.access.redhat.com/ubi8/ubi:8.1

RUN curl -L -o cincinnati-graph-data.tar.gz https://api.openshift.com/api/upgrades_info/graph-data

RUN mkdir -p /var/lib/cincinnati-graph-data && tar xvzf cincinnati-graph-data.tar.gz -C /var/lib/cincinnati-graph-data/ --no-overwrite-dir --no-same-owner

RUN [ -f /var/lib/cincinnati-graph-data/products.json ] || \
      curl -L -o /var/lib/cincinnati-graph-data/products.json https://access.redhat.com/product-life-cycles/api/v2/products || \
      echo "{}" > /var/lib/cincinnati-graph-data/products.json


CMD ["/bin/bash", "-c" ,"exec cp -rpv /var/lib/cincinnati-graph-data/* /var/lib/cincinnati/graph-data"]
