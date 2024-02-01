# Story proxy

```sh
$ curl -x POST https://{host}/v1/story -d '{"name": "blah"}' -h "Authorization: bearer <token>"
-> {"id": "123456"}

$ curl -x PUT https://api.{host}/v1/story/{id} "Authorization: bearer <token>" -F ...

$ curl -x PATCH https://api.{host}/v1/story/{id} "Authorization: bearer <token>" -F ...
```

Bucket path: data.nav.no/blah/* -> https://data.nav.no/blah
