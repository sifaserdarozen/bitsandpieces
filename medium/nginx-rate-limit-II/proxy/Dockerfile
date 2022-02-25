FROM nginx:1.21.0-alpine as BUILD
RUN apk update && apk add --no-cache gcc g++ pcre-dev zlib-dev make
WORKDIR /tmp
COPY ./src/. .

EXPOSE 80

RUN \
    wget "http://nginx.org/download/nginx-1.21.0.tar.gz" && \
    tar -xzvf nginx-1.21.0.tar.gz && \
    cd nginx-1.21.0 && \
    ./configure --with-compat --add-dynamic-module=/tmp/ngx_extended_monitoring && \
    make modules && \
    cp objs/ngx_extended_monitoring_module.so /etc/nginx/modules/

CMD ["nginx", "-g", "daemon off;"]


