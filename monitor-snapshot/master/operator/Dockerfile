<<<<<<< HEAD
FROM busybox
=======
FROM busybox:1.36.1
>>>>>>> 418c0bc (chore(deps): update busybox docker tag to v1.36.1 (#342))

ADD init.sh /usr/bin/init.sh
RUN chmod +x /usr/bin/init.sh

COPY dashboards/*.json /tmp/
COPY rules/*.rules.yml /tmp/
COPY datasources/*.yaml /tmp/

ENTRYPOINT ["/usr/bin/init.sh"]
CMD ["TIDB-Cluster", "/grafana-dashboard-definitions/tidb/", "false", "/etc/prometheus"]