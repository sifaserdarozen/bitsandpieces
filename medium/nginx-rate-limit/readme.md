# Experiment around nginx rate limit

### What is it?
Nginx is a lightweight proxy that can be used to eliminate direct application exposure.
It has also handy rate limit functionality that can be used as basic circuit breaker to avoid
overloading. Besides, rate limiting can as well serve as a security mitigation against denial of service attacs.


### Build images
Build web server image in **web** directory 
```
cd web
docker build -t ratetest-web:0.1 -f ./Dockerfile .
```


### Setup environment
```
docker-compose up
```


### Smoke test
```
curl -w "\n" localhost:8080
```


### Load test
A simple loadtest can be run through
```
for i in {1..20}; do curl -s -o /dev/null -w "%{http_code}" localhost:8080; echo ""; done
```


### Teardown environment
```
docker-compose down
```


### License
MIT
