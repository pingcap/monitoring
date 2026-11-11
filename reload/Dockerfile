FROM busybox:1.38.0

# Add user and group to support run it with non-root.
ARG PINGCAP_UID=1000
ARG PINGCAP_GID=2000
RUN addgroup -g ${PINGCAP_GID} pingcap && \
    adduser -u ${PINGCAP_UID} -G pingcap -h /home/pingcap -D pingcap

COPY --chmod=755 ./build/linux/reload  /bin/reload
# /bin/reload prepares legacy volumes, then execs as the pingcap account before serving.
USER 0:0
ENTRYPOINT ["/bin/reload"]
CMD        ["--watch-path=/etc/prometheus/rules", "--prometheus-url=http://127.0.0.1:9090"]
