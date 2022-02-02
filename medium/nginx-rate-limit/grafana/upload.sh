#!/bin/bash

for i in datasources/*; do \
    curl -X "POST" "http://localhost:3000/api/datasources" \
    -H "Content-Type: application/json" \
     --user admin:admin \
     --data-binary @$i
done

for i in dashboards/*; do \
    curl -X "POST" "http://localhost:3000/api/dashboards/import" \
    -H "Content-Type: application/json" \
    --user admin:admin \
    -d "{\"dashboard\":$(cat $i)}"
done