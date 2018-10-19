# devcache

[![GitHub](https://img.shields.io/github/license/travis-g/devcache.svg)](https://github.com/travis-g/devcache)

A rudimentary server for proxying HTTP requests and caching _static_ responses.

```sh
go get github.com/travis-g/devcache
```

The cache itself saves to disc if the server is sent SIGINT and will attempt to load a cache from the current working directory at startup: it's helpful to keep a separate cache per API.
