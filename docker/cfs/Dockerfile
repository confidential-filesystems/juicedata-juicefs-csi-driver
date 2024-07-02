FROM golang:1.21.7 as builder

ENV GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
RUN apt-get update && apt-get install -y btrfs-progs libbtrfs-dev libdevmapper-dev libgpgme-dev libselinux1 libblkid1 libpcre3 uuid-dev libpcre2-8-0

FROM gcr.io/distroless/base-debian12:debug

WORKDIR /confidential-filesystems/juicefs-csi-driver

COPY --from=builder /usr/lib/x86_64-linux-gnu/libgpgme.so.11 /usr/lib/x86_64-linux-gnu/
COPY --from=builder /usr/lib/x86_64-linux-gnu/libassuan.so.0 /usr/lib/x86_64-linux-gnu/
COPY --from=builder /usr/lib/x86_64-linux-gnu/libgpg-error.so /usr/lib/x86_64-linux-gnu/libgpg-error.so.0
#COPY --from=builder /usr/lib/x86_64-linux-gnu/libdevmapper.so* /usr/lib/x86_64-linux-gnu/
COPY --from=builder /lib/x86_64-linux-gnu/libudev.so* /lib/x86_64-linux-gnu/
COPY --from=builder /usr/lib/x86_64-linux-gnu/libdevmapper.so /usr/lib/x86_64-linux-gnu/libdevmapper.so.1.02.1
COPY --from=builder /usr/lib/x86_64-linux-gnu/libselinux.so /usr/lib/x86_64-linux-gnu/libselinux.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libblkid.so* /lib/x86_64-linux-gnu/
COPY --from=builder /lib/x86_64-linux-gnu/libpcre.so* /lib/x86_64-linux-gnu/
COPY --from=builder /usr/lib/x86_64-linux-gnu/libuuid.so /usr/lib/x86_64-linux-gnu/libuuid.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libc.so.6 /lib/x86_64-linux-gnu/
COPY --from=builder /lib64/ld-linux-x86-64.so.2 /lib64/
COPY --from=builder /usr/lib/x86_64-linux-gnu/libpcre2-8.so.0 /usr/lib/x86_64-linux-gnu/

COPY ./juicefs-csi-driver /usr/local/bin/juicefs-csi-driver

CMD ["juicefs-csi-driver", "--endpoint=${CSI_ENDPOINT}", "--logtostderr", "--nodeid=${NODE_NAME}", "--leader-election", "--leader-election-namespace=${CFS_MOUNT_NAMESPACE}", "--v=5", "--provisioner", "--webhook=true", "--webhook-cert-dir=/etc/cfs/conf/certs"]
