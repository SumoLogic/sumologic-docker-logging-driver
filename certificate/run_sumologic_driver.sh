docker container run --rm --log-driver=store/sumologic/docker-logging-driver:1.0.2 \
    --log-opt sumo-url=http://127.0.0.1:80 \
    --log-opt sumo-source-category=test-ubuntu-docker-ee \
    --log-opt tag={{.ID}} \
    --log-opt sumo-batch-size=2000000 \
    --log-opt sumo-queue-size=400 \
    --log-opt sumo-sending-interval=2000ms \
    --log-opt sumo-compress=false \
    --volume $(pwd)/quotes.txt:/quotes.txt alpine:latest \
    sh -c 'cat /quotes.txt;sleep 10'

