FROM busybox:1.37.0@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec

# Add user and group to support run it with non-root.
ARG PINGCAP_UID=1000
ARG PINGCAP_GID=2000
ENV RELOADER_UID=${PINGCAP_UID}
RUN addgroup -g ${PINGCAP_GID} pingcap && \
    adduser -u ${PINGCAP_UID} -G pingcap -h /home/pingcap -D pingcap

COPY --chmod=755 init.sh /usr/bin/init.sh
COPY --chmod=644 dashboards/*.json rules/*.rules.yml datasources/*.yaml /tmp/

ENTRYPOINT ["/usr/bin/init.sh"]
CMD ["TIDB-Cluster", "/grafana-dashboard-definitions/tidb/", "false", "/etc/prometheus"]
