# FTPS with implicit TLS (the "port 990" style): TLS starts as soon as the
# client connects, on the control and on the data connections.
FROM alpine:3.22
RUN apk add --no-cache vsftpd openssl \
 && adduser -D -h /home/skiff skiff \
 && echo 'skiff:skiff' | chpasswd \
 && openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj '/CN=localhost/O=Skiff/C=FR' \
      -keyout /etc/vsftpd/key.pem -out /etc/vsftpd/cert.pem
COPY vsftpd.conf /etc/vsftpd/vsftpd.conf
EXPOSE 990 30010-30019
CMD ["vsftpd", "/etc/vsftpd/vsftpd.conf"]
