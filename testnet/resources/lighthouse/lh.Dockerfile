FROM rust:1.53.0 AS builder

RUN apt-get update && apt-get -y upgrade && apt-get install -y cmake

RUN curl -sL https://deb.nodesource.com/setup_14.x | bash - \
    && curl -sS https://dl.yarnpkg.com/debian/pubkey.gpg | apt-key add - \
    && echo "deb https://dl.yarnpkg.com/debian/ stable main" | tee /etc/apt/sources.list.d/yarn.list \
    && apt-get update \
    && apt-get install -y nodejs yarn --no-install-recommends

RUN npm install -g ganache-cli

COPY . lighthouse
WORKDIR lighthouse

ARG PORTABLE=true
ENV PORTABLE $PORTABLE
RUN make
RUN make install-lcli

#CMD ["./scripts/local_testnet/start_local_testnet.sh"]
