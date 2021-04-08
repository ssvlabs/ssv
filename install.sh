# install docker
sudo apt-get update
sudo apt-get install \
    apt-transport-https \
    ca-certificates \
    curl \
    gnupg \
    lsb-release

curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
echo \
  "deb [arch=amd64 signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt-get install docker-ce docker-ce-cli containerd.io

# install make
sudo apt-get update && \
  DEBIAN_FRONTEND=noninteractive sudo apt-get install -yq --no-install-recommends \
  bash make curl git zip unzip wget g++ python gcc-aarch64-linux-gnu \
  && rm -rf /var/lib/apt/lists/*

# run make
make docker-image