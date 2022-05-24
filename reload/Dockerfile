FROM alpine:3.14.6

COPY ./build/linux/reload  /bin/reload
ENTRYPOINT [ "/bin/reload" ]
CMD        [ "--watch-path=/etc/prometheus/rules", \
             "--prometheus-url=http://127.0.0.1:9090" ]
