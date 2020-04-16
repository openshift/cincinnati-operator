FROM registry.access.redhat.com/ubi8/ubi:8.1

RUN curl -o cincinnati-graph-data.tar.gz https://codeload.github.com/openshift/cincinnati-graph-data/tar.gz/master

CMD exec /bin/bash -c "tar xvzf cincinnati-graph-data.tar.gz -C /var/lib/cincinnati/graph-data/ --strip-components=1"
