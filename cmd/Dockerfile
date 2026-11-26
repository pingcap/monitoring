FROM busybox

# Add user and group to support run it with non-root.
ARG PINGCAP_UID=1000
ARG PINGCAP_GID=2000
ENV RELOADER_UID=${PINGCAP_UID}
RUN addgroup -g ${PINGCAP_GID} pingcap && \
    adduser -u ${PINGCAP_UID} -G pingcap -h /home/pingcap -D pingcap

ADD init.sh /usr/bin/init.sh
RUN chmod +x /usr/bin/init.sh

COPY dashboards/*.json /tmp/
COPY rules/*.rules.yml /tmp/
COPY datasources/*.yaml /tmp/

ENTRYPOINT ["/usr/bin/init.sh"]
CMD ["TIDB-Cluster", "/grafana-dashboard-definitions/tidb/", "false", "/etc/prometheus"]
