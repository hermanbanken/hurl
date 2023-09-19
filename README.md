# hURL - lets hurl those urls at you
Or, cURL but from Herman.

Live adjust parallelism (p) and rate (r) while the list of urls is being processed:
```
go install github.com/hermanbanken/hurl@latest
hurl urls.txt
p=20
p=10
p=100
```
