FROM busybox:1.38.0@sha256:a46257e06c82ad85242d0d38077d0a52d9896389ca60a1a59d3463db586e5fa4

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
