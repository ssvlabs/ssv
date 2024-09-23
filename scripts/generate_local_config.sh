#!/bin/bash
##### ./generate_local_config.sh 4 ./keystore-m_12381_3600_0_0_0-1639058279.json 12345678 0x0000000000000000000000000000000000000000 0
function extract_pubkey() {
    LOGFILE=$1
    grep "generated public key (base64)" "$LOGFILE" | grep -E -o '(\{.+\})' | jq -r ".pk"
}

function extract_privkey() {
    LOGFILE=$1
    grep "generated private key (base64)" "$LOGFILE" | grep -E -o '(\{.+\})' | jq -r ".sk"
}

function create_operators() {
  OP_SIZE=$1
  echo "creating $OP_SIZE ssv operators"
  rm operators.yaml 2> /dev/null
  touch operators.yaml
  rm -rf config 2> /dev/null
  mkdir -p config

  for ((i=1;i<=OP_SIZE;i++)); do
    docker run --rm -it 'ssvlabs/ssv-node:latest' /go/bin/ssvnode generate-operator-keys > tmp.log
    PUB="$(extract_pubkey "tmp.log")"
    val="$PUB" yq e '.publicKeys += [env(val)]' -i "./operators.yaml"
    PRIV="$(extract_privkey "tmp.log")"
    touch "./config/share$i.yaml"
    val="./data/db/$i" yq e '.db.Path = env(val)' -n | tee "./config/share$i.yaml" > /dev/null \
      && val="1500$i" yq e '.MetricsAPIPort = env(val)' -i "./config/share$i.yaml" \
      &&  val="$PRIV" yq e '.OperatorPrivateKey = env(val)' -i "./config/share$i.yaml"
  done
  echo "share.yaml(s) generated"
  rm tmp.log
}

OP_SIZE=$1
KS_PATH=$2
KS_PASSWORD=$3
OA=$4
NONCE=$5
SSV_KEYS_PATH=${6:-'./bin/ssv-keys-mac'}

create_operators "$1"

rm -rf key_shares 2> /dev/null
mkdir -p key_shares
rm tmp.log 2> /dev/null

for ((i=1; i <= OP_SIZE; i++))
do
  OID+="$i"
  if [ "$i" -lt "$OP_SIZE" ]; then
      OID+=","
  fi
done

echo "generating ssv keys"
$SSV_KEYS_PATH -of=./key_shares -ks="${KS_PATH}" -ps="${KS_PASSWORD}" -oids="${OID}" -oks="$(yq e '.publicKeys | join(",")' operators.yaml)" -oa="${OA}" -on="${NONCE}" > tmp.log
KEY_SHARES_PATH=$(grep -o './key_shares.*json' tmp.log)

rm temp.yaml 2> /dev/null
touch temp.yaml
rm tmp.log 2> /dev/null

echo "populating events.yaml"
for ((i=0;i<OP_SIZE;i++)); do
  ID=$(ii=$i yq e '.data.operators[env(ii)].id' "$KEY_SHARES_PATH")
  PK=$(ii=$i yq e '.data.operators[env(ii)].operatorKey' "$KEY_SHARES_PATH")
  yq e -i '.operators += [{"Log":"","Name":"OperatorAdded"}]' temp.yaml
  ID=${ID} ii=$i yq e -i '.operators[env(ii)].Data.ID = env(ID)' temp.yaml
  PK=${PK} ii=$i yq e -i '.operators[env(ii)].Data.PublicKey = env(PK)' temp.yaml
done

yq e -i '.validators += [{"Log":"","Name":"ValidatorAdded"}]' temp.yaml
PK=$(yq e '.data.publicKey' "$KEY_SHARES_PATH")
OIDS=$(yq e '.payload.operatorIds' "$KEY_SHARES_PATH")
SHARES=$(yq e '.payload.sharesData' "$KEY_SHARES_PATH")

OIDS="${OIDS}" ii=$i yq e -i '.validators[0].Data.OperatorIds = env(OIDS)' temp.yaml
OA=${OA} yq e -i '.validators[0].Data.Owner = env(OA)' temp.yaml
PK=${PK} ii=$i yq e -i '.validators[0].Data.PublicKey = env(PK)' temp.yaml
SHARES=${SHARES} ii=$i yq e -i '.validators[0].Data.Shares = env(SHARES)' temp.yaml

rm ./config/events.yaml 2> /dev/null
touch ./config/events.yaml
yq '.operators, .validators' temp.yaml > ./config/events.yaml
rm temp.yaml 2> /dev/null
rm operators.yaml 2> /dev/null
rm -rf key_shares 2> /dev/null
echo "./config is ready"
