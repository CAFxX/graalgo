package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

const caPEM = `-----BEGIN CERTIFICATE-----
MIIDJTCCAg2gAwIBAgIUMvYGu2nHBoGwwEPo3ZdaWgKuyqIwDQYJKoZIhvcNAQEL
BQAwGjEYMBYGA1UEAwwPZ3JhYWxnby10ZXN0LWNhMB4XDTI2MTAzMDE4NTQ1MFoX
DTM2MTAyNzE4NTQ1MFowGjEYMBYGA1UEAwwPZ3JhYWxnby10ZXN0LWNhMIIBIjAN
BgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAwG8eBxBDtL7LzWUn51ddEcijs9DM
5FvQmuX71JGEZg2pSKjlrHryGF17JoInQm1+tL9Hqng1iU7IJtKz0MszAEyzv9En
rrBvjZfPaEkArfHcbBkLE6arL2Hl1YpJ0rVrjy2I24EVeqEkimrvs3FEVb3qtJE3
DpF5Qp0zxt5j3kCFGHm8KIiu3Aw7en5fpHmcgQmfnqiji5uOXvZKO/eoOuyw49hT
HLghOLtIgQQKH3T/YIW/xjLTKjo3T9LaBktJjUX1g7Z5utbCncZdKf8cOZEy4MjN
PQVp074o+rIO0vDj7YsL9RFqziA3hwzB6z8U84498Ul0VSJkQUgTmK/J5wIDAQAB
o2MwYTAdBgNVHQ4EFgQU1Dyu/E/B4GCiID17sa7ESCh0+mwwHwYDVR0jBBgwFoAU
1Dyu/E/B4GCiID17sa7ESCh0+mwwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8E
BAMCAQYwDQYJKoZIhvcNAQELBQADggEBAEaA3Pr5Bj8VYsDioQLrcR6PG/iGFz5M
GwbDArai/BLfKhVvyQOv+NDvP9Fw6H0nsO04hVghOOi825RHFJwKOagzi8fKLXYR
wCys/e3bI65vY801JcZ4Ws9eqH62MsP4nzUwBGp1JxUUnFVepDY0CvP7EjKtJU3n
leoBzBCTypD95ujRzQ99RDjF5UMPHWuKrGR192H51FXaYYjM2Wd6rvWcCbnjGPgY
0Nqenp0rg2EVplz7nPoQnBX5k1ueWI4Mh9pP0ICtPxhyV77Rv7GcagHhN6lUvo6B
jXBqGznPmSH8U3YbSSknBiBSV3gZSUBb3LkCOrpBojIUIobOwhLNtUs=
-----END CERTIFICATE-----`

const srvPEM = `-----BEGIN CERTIFICATE-----
MIIDOjCCAiKgAwIBAgIUTHQLhAqcV7T5N3lDQG2qmBQklRIwDQYJKoZIhvcNAQEL
BQAwGjEYMBYGA1UEAwwPZ3JhYWxnby10ZXN0LWNhMB4XDTI2MTAzMDE4NTQ1MloX
DTM2MTAyNzE4NTQ1MlowFDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG
9w0BAQEFAAOCAQ8AMIIBCgKCAQEAly2YgTrX3lw7HBEVHLqft5bOxgVBR+pTVSdk
+ifgQ4sxpWr2uguk08MbEPgGrhhDXGZxHvQrGqPggLx8Gj7QsLtxKKj9o4YjlWix
4ZIWbWwYDQNH6x4Uun4BpnA5IqdN/2pUwKmPazS+yHqwMAAoTRFYigwHicph/3eQ
6FFsyLIEQU6j+KlOfbU9vAh8xkpsjnhrhoy9Nkhy/MW1gTkr6F1hsuh4hPk30jYd
RWtJxhufceH2wxiV5siBmfAuqblp2JqhzoYBRjWbJ2bDd/r8KV73B5OMeUa3hbad
JpAI6Avp85AThUbkSBzKZgz6IcYb2gauOc+8fLbU+RCoQkhTZwIDAQABo34wfDAa
BgNVHREEEzARgglsb2NhbGhvc3SHBH8AAAEwEwYDVR0lBAwwCgYIKwYBBQUHAwEw
CQYDVR0TBAIwADAdBgNVHQ4EFgQUXsw+eRzJxiXESOy+fWgv10QF/x8wHwYDVR0j
BBgwFoAU1Dyu/E/B4GCiID17sa7ESCh0+mwwDQYJKoZIhvcNAQELBQADggEBAAj3
YgBToGjffXwQk07PwOWHiZYYVuvLSm1kjuevPmsWpXH3sjOf42wHjG+qVSZOJa3/
Ow7WhF5pgEAmfe7GdgbKtF6lx0/rOdUVk3bEfp4BoIDtbKgQhPR8kbVsaDMoYqyw
6T7XQcM7tlzdydjw6o6Vo9O53SchvPWmk2PrKe8z4WUfChXzKO5Aq/D0VK3rmAEv
xsUAJ9Y82zCD/EzitMBFd5xMKCeApe/OKB/9N0HzGnLMLocgQjjnJ5pDcpOTGcH8
I+JGzALN2TNNDZQr0Vd67RK6xX5dP/DwCz1ZPoKGMQ6ZuMWCxrmc2tP/cA0WiE9v
zRW6jM4B0caDp2n9BmM=
-----END CERTIFICATE-----`

const srvKeyPEM = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCXLZiBOtfeXDsc
ERUcup+3ls7GBUFH6lNVJ2T6J+BDizGlava6C6TTwxsQ+AauGENcZnEe9Csao+CA
vHwaPtCwu3EoqP2jhiOVaLHhkhZtbBgNA0frHhS6fgGmcDkip03/alTAqY9rNL7I
erAwAChNEViKDAeJymH/d5DoUWzIsgRBTqP4qU59tT28CHzGSmyOeGuGjL02SHL8
xbWBOSvoXWGy6HiE+TfSNh1Fa0nGG59x4fbDGJXmyIGZ8C6puWnYmqHOhgFGNZsn
ZsN3+vwpXvcHk4x5RreFtp0mkAjoC+nzkBOFRuRIHMpmDPohxhvaBq45z7x8ttT5
EKhCSFNnAgMBAAECggEAHpA7yEiestg/pQV5GLVMLAAjrpoKYCEqFsmfmwlryAwA
jfpJhkRrXtqPgsc6BoFmQOx/Cyx++GRQM4anqmCvrUM/9eHwYVpxoDep2ro/qYCU
fjD/HbQXqbpm2ADRNEXRuYUEPG0rTxe7SGmxbArkGuyaU1JqRbuyQ0UneShZD1vK
QtUn+/9A5OOk6n4WegVOQKFudNWeY+JXsxbjIpPQMjlxGuNJupxn66yoFXLpmwjz
VW84b/UhsXrs1XVPaZNOCrIesh/8TK2ZI6zUvj/bGB3hBKYiqTjdP/31DJ9zy70C
wJbJHBw3Elnq2mNFlz5lX6WqFTbV9m9kAuwFHyVE5QKBgQDUjzIC8HhmPa6g18cq
jDPpF5eB6wNY6b4mVu67CT/Vi6/DkCHbtwhnAojAvGx439KQxSapv9VQf/Zg0mDi
ln7PB204LvS2rB2PHsf/00MlZDjGAic1mRd/zF8xzHM2zGjt3uec/NI5jub+vg4s
V3fADofxtpYX9lYIMICwFqVBgwKBgQC2EwU0jG6uG+dQ2WptoeCj1D7/Lz0Cdu/N
nrpPhcLHIZI0Lhwlfwnu2E2Aqg30DlZ1lUsry7FfJTZhk42eYwg4wrPVkVVxz8zL
GTpBZVAQ94pGL/Tb5bDJv7asZ+h4grOBNqTRLBBTsEYvlL+r3IbI4hc+/jBMHT48
HhGp/ha1TQKBgDwD0lMSImx7rfqr8ggJXf7KMnT0LhXEqXOZx/33NpbMweutnmGy
5CRxirsgeGAIkiNeBvSWvVvlRWNBAyn25AcASc47M8z/+7iVMFMDC0pbacw+f2Oa
vg/PXHFrEHzAaj//V9XzbEfkGUSD1OygJaeMcsXEj8xkORZG247cVfyRAoGAf8WT
RpAyb76nMsaQeIU8JtwgpWXaE6uxl4OHTZ0Z1jKJ7lJZcDrZlqJ2qQ8toZKaPAPb
780PGSz9H7azkG4A7Aw+tWY7JxZjqjd2+UOGz5FXmO+4jsR+ruX55L06l69pFwlo
2l1oy8INTwC9DXdBoyF+nLc234M6FGgpjzj/aJUCgYEAi9m5I6iDMVCgH6+wVeaM
oDhiYm1smvNRb3as5qnmrK7x8fJXOOvIQOKPKYJ7mUb/BV9o1TbIzyX6i+2uzMZy
sV1wpjxE5F2y5uRjgzVhO8xVLsYQhirZ1N1vIxHJWKP1WVpMuVvUu+sLhQHuRiGA
fQ4ZGrd7W8FrWAfVU0mtLHQ=
-----END PRIVATE KEY-----`

const cliPEM = `-----BEGIN CERTIFICATE-----
MIIDKDCCAhCgAwIBAgIUTHQLhAqcV7T5N3lDQG2qmBQklRMwDQYJKoZIhvcNAQEL
BQAwGjEYMBYGA1UEAwwPZ3JhYWxnby10ZXN0LWNhMB4XDTI2MTAzMDE4NTQ1M1oX
DTM2MTAyNzE4NTQ1M1owHjEcMBoGA1UEAwwTZ3JhYWxnby10ZXN0LWNsaWVudDCC
ASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAMFEfr45SnrWIAfadjO6iLyM
M0b5RZhW/nPUoO4Z4gXiGUMwmKPJRyQD49NBSqbkktkf+xhYsmzXRtOHBtm10Rrg
6d+HQMkZlc5DdgLFLOXggQV/Gm2fRk8zn33dLHzTuEAtvwgqYaE7BVfI7O4KzZrK
RQCdCNob5MMVWXAfi7qURxY+NvajdrNKekvmt39xrww+iq7M/Bl1YP3i3wKNFXRb
t8HaIJtjSaLnJNDRCOzj6nGz1IqzKFR2YLes7lV8Ul3kJR8yVS6aFgLmx2Zgdgdd
01xUwPuD2TnbOLdrdXOV7pOp1p5KWxboMJLD/xosxAqJhmmHAuFtGJbNWuMjgY8C
AwEAAaNiMGAwEwYDVR0lBAwwCgYIKwYBBQUHAwIwCQYDVR0TBAIwADAdBgNVHQ4E
FgQUKzmzrlgcjNmbTdYNQ86VrUUec68wHwYDVR0jBBgwFoAU1Dyu/E/B4GCiID17
sa7ESCh0+mwwDQYJKoZIhvcNAQELBQADggEBAIYcV13aNkbGAkQt72VT2WjepQYU
2iI6Q4iJlvhi97Q3SiL4ccoKNML1s0KChUJY1QFbPFifdvRRt0i1qjqDBlfdWgpn
2sk3KlEtWtyzXCSTzWqEEVnQEfTsWVYAnAc/VGfCti3xkBvMCb5zkhHwFBra0U9f
Wvej7pm8B4iSjGmcqfcyJUQVhKZD3J0VqjXfmq2P/wsBjzqJH5pAhPfK7rpFbKsX
VxlImTgZ2mOMb7vF0ZqQrBhFPEtEcO6Tx6xuGLATcB14NlX6LaqBmHpgTt5UyWKs
pV2i8l2leAXckUyhgpmxzbaF/h0Agg1DUdqi2p2vAzdI5me3AX8t2WNcSRk=
-----END CERTIFICATE-----`

const cliKeyPEM = `-----BEGIN PRIVATE KEY-----
MIIEvwIBADANBgkqhkiG9w0BAQEFAASCBKkwggSlAgEAAoIBAQDBRH6+OUp61iAH
2nYzuoi8jDNG+UWYVv5z1KDuGeIF4hlDMJijyUckA+PTQUqm5JLZH/sYWLJs10bT
hwbZtdEa4Onfh0DJGZXOQ3YCxSzl4IEFfxptn0ZPM5993Sx807hALb8IKmGhOwVX
yOzuCs2aykUAnQjaG+TDFVlwH4u6lEcWPjb2o3azSnpL5rd/ca8MPoquzPwZdWD9
4t8CjRV0W7fB2iCbY0mi5yTQ0Qjs4+pxs9SKsyhUdmC3rO5VfFJd5CUfMlUumhYC
5sdmYHYHXdNcVMD7g9k52zi3a3Vzle6TqdaeSlsW6DCSw/8aLMQKiYZphwLhbRiW
zVrjI4GPAgMBAAECggEAF0nCn/dVUzHkEC4x2g/bFCUstJuq2C17Yce4UJiL1Lum
4aLEcc/6I4vYcwdGEJAwZbtlzUw6y8SxAyIdIDS5aqy5YbbxhonvtDgKy1CpFVYV
Fe136MeteDO3PzpcdvooFIuunjRFKWFsRmSusAxbNBC1edpyS53a1esHqOwUMToh
IrWNIuHStA8/iA2vQ1gJYu5/ModkXBAw1wXErTlpY9SLsGYISyc0Z1baiy97ISxm
GmMZpFEwOpdPNv8Ru093qmBMpOzLKqWdj8kOny+njggkahqI3vUDQyCMRvUmqE2m
v1xuv96uv+fTe2Dku8DNr1I5Y/ieEKCAbsxyGKwAOQKBgQDoTmar1GLTEUEc44D/
XS6zVhRxS3Gxvx0e2r2jeFogLYjAaBUoNUsbcSMozSuuBiy2t27Lo85pBun9miCZ
/L9YpG9WDnexRHannbyfO910Ium0e3vLM7LTlXevj/w1m5Lk9b42MRjtd9+1Pu9j
hgB+OwCnRu+D32eeF5XjqyACywKBgQDU+sehrqdJOYZIQwunTuxO1KxIfV5fqzVQ
yHRO7PjQWG3GVl/nJZFWqoMOZMR/eq8ObBKHDoulict6SK+88ff506aRoIz+YJDu
K+VBvEih0UW3hhSb3wf2Xnh/JzGaoVBRpdtZfM3jBJP+WhMrbRe0tNusyNw4Q+hV
Vj67CDovzQKBgQCQMlo4vpS5PzSTogQofsbGOuBlktb35hFFXbHpDoOjBaClDfZU
BwCgABI6+V/rnZmTsTjs3rj5KHmGGW2zJ9f+RdN2L3TMbBra4GZ80MQdmq5GgX/O
GkEeZODt8Qo0nGUKCiAPs1otzQLnljEyLbOQpz84lieGS94dm7ZGo6ozEQKBgQCE
JfaoQ45YyPCg9bjNXxMQ9rTPZbqg8cJrFXXVYwQfgj7ayc1Hxu+fLr2SuMOTtbPY
5wW39zZ+yDkQ7vjb6+dJhCHeShgx3gkPPv1JBGKsxQTVEsTG0mRGYeeyPcdxgpYr
kxW/OMknCzgoogsy7sW+wllrpFXAM6J9EL5rnnc8IQKBgQDSSkF3eTB/GgSRdY8b
iGNSZml1N93d77pSZhSTzeCtQq4vUzf05c/uql4Of7rDcbCqs0+MDbSw7Dewi8lD
rbs7XSjg9LJqW/D0bEz5SM2G/+s3D5E5q8GrkKTSBI6n1RdvoMJd+cbghxOlaArv
TwP6umwy/CORdr924Z0+I891FQ==
-----END PRIVATE KEY-----`

const expPEM = `-----BEGIN CERTIFICATE-----
MIIDDTCCAfWgAwIBAgICEJIwDQYJKoZIhvcNAQELBQAwGjEYMBYGA1UEAwwPZ3Jh
YWxnby10ZXN0LWNhMB4XDTI2MTAyODE5MDA0NFoXDTI2MTAyOTE5MDA0NFowITEf
MB0GA1UEAxMWZ3JhYWxnby1leHBpcmVkLWNsaWVudDCCASIwDQYJKoZIhvcNAQEB
BQADggEPADCCAQoCggEBANfibM3dYMIE7c566Ay7jb0BZW/EsMg9H1Rr/Hgb1q8S
5lBUVQnd+LzlVb9ZaBPZgP4LD9hi5vGZfImCxrJWEAmF/9ykKfEOtV29tdupB/8I
MAP4YNqLk7ccMnK7ZsMwBrMHHUtrUVxLyGafRnYtzrlHbSpc/omtQXzv4a2sx1wv
rYNBHqkbPDhBz6fJgLjbOOW6tbxfXBckjerqp81kqgQfnf4yT+vMURBJBBYRihqc
hT5BtBHQlp6gmOlAcK/poDcu7R8u0NQCk6NrmeK1PLGMTdumtzdWC0hAByTk1ez4
d62USTOcWaEfuVckfNw989KHsBD99SPE3Njhgia6C0ECAwEAAaNWMFQwDgYDVR0P
AQH/BAQDAgeAMBMGA1UdJQQMMAoGCCsGAQUFBwMCMAwGA1UdEwEB/wQCMAAwHwYD
VR0jBBgwFoAU1Dyu/E/B4GCiID17sa7ESCh0+mwwDQYJKoZIhvcNAQELBQADggEB
AKTBfOKab7Q2BWY8+hYp28tptr44fhnTunUN1hZMO4uVxBrbpLXqJMlWv5Px1Lyi
r8kcv93CiF9lGUYCRrXZ87WYVXDYHJWqBUh4WQSYhVqphFcJU0opIuSnv4qMk6WI
ZTM20NQ4NRLfRYL6tpeJhktJRgWstJ9UvpoU2b49QrHr8kwfU/aL1Rrxec6lU5dN
qmPTFKEFhssfi5XQn2vvKycl2i3lNww9/R1pswQoirdY3RJEHaB16OvHtHvMfEwo
WznRovOcYVXqM+agbaKJbghdvOter5Ti4WjYlD1z478sR9LELcNN7dNKiveqVYQ9
heL4I4c9GoqYIvQtuIt6DvU=
-----END CERTIFICATE-----`

const expKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEpQIBAAKCAQEA1+Jszd1gwgTtznroDLuNvQFlb8SwyD0fVGv8eBvWrxLmUFRV
Cd34vOVVv1loE9mA/gsP2GLm8Zl8iYLGslYQCYX/3KQp8Q61Xb2126kH/wgwA/hg
2ouTtxwycrtmwzAGswcdS2tRXEvIZp9Gdi3OuUdtKlz+ia1BfO/hrazHXC+tg0Ee
qRs8OEHPp8mAuNs45bq1vF9cFySN6uqnzWSqBB+d/jJP68xREEkEFhGKGpyFPkG0
EdCWnqCY6UBwr+mgNy7tHy7Q1AKTo2uZ4rU8sYxN26a3N1YLSEAHJOTV7Ph3rZRJ
M5xZoR+5VyR83D3z0oewEP31I8Tc2OGCJroLQQIDAQABAoIBAC2Ts/zSW/caK0qI
UC0/+3CHtu6dy+S6Yr7j/W0uE2jqyIz1AO8QWc8na672JS7roHwBwdqhJrii+tPd
NC26VbBtBdODJQerX0bnIlm5meKRy/FKosgA57Kk1VG7lrpmjUOGoPynL4oYxbpE
rg+NJDOhjL6A24wdc9H4ouCTtZMUaj5gGFmM+W1pkcpl0o3OF5quiGwiyrw6GlIy
EPAAcUkhnRVtvOFBu0OV+pbs3sBREoTVf1rl4gjwNykyn4+YOOKcGdG0dLh3X6fC
XcnpzpXrE8i/MdqTdTRNyrOrJKUAyEGrMYuYHsNsOtvZ2pAsJi9aXoumFxDfLOr8
V4vz5jECgYEA/E01oQHVO/B/q9RmrDjEPnWlx2i+t9rMfr6FbDJ1C6/2UnoQ9T6e
gem5QE5T8UPYpXVfBkFyluPGTJQJiaK92t8PcPqaoh76sQoO+RYMaSsicQA5enXQ
xSJYTG5hFXtfgWu8KUi1/JjgX5fKz7oZUtJb7Rl/tkWEbu/5SlY4K/8CgYEA2wyO
XSH1DaHF2u04/zX+dHc4nspthYov0yVbesV3VCRMHNVqNg9qaeF8xyr/b0wl4nzg
h3gy1X35mj0zx7RKCDJcF07e0EDoWs6ci0HmBe5K5SZFAHqrPsBh3SysmXAXRf6U
WKGsXr4up/lvUQcUTZnOGqGOVFmomiCv7HeOyL8CgYEAttsWhSyY6KozPcvg5ez2
ffYyQkXuA741+54FgRdIsUHEmp4RcE0EAE/ujWa3e+KaP8DDL/t7G1TVwWaBSxmB
1AFaZZ2bOOiO9IS/uVN7hzVV7uQkxFZjCp0LtclB35nc7CWy55DlJL9/zohq2wab
l+UR7tNONkJH60dNO80lRO0CgYEAgPZLeplbZMMxcUrom5h2TqtCMnqLMGvC7FS6
HvXS4z7bcL5U4a8Bv4R5qeiuqh/yzf0nU5p4kyivoxcVPSXv0BOWFELi+0i7g3rQ
v0v02NkKTw7Q4Q9XcoJhG44Jr+xGR3cyb1v/12ewoERCPTolweojZZpn6VE/6j4E
VPuunJECgYEAnUq10CNOKBSE9fwAKPADy0zAUHWeCZEwBrVbxI/xPXVzZiZsnaRf
NKWavzU9pUurJHgZ/ZuBJfJsomzN90hKanEHYZKkTpQrD/3rPSubvmz1gThDkX5v
wXn1D+wBfR4uTL+ncbgeWYdekQykDF4cfjzN1NJk7oCgNHbDLyVTsVM=
-----END RSA PRIVATE KEY-----`

func mustPair(certPEM, keyPEM string) tls.Certificate {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		panic("X509KeyPair: " + err.Error())
	}
	return pair
}

func caPool() *x509.CertPool {
	block, _ := pem.Decode([]byte(caPEM))
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		panic("parse ca: " + err.Error())
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return pool
}

func serverConfig(auth tls.ClientAuthType, cas *x509.CertPool) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{mustPair(srvPEM, srvKeyPEM)},
		ClientAuth:   auth,
		ClientCAs:    cas,
	}
}

func clientConfig(cert *tls.Certificate) *tls.Config {
	cfg := &tls.Config{RootCAs: caPool(), ServerName: "localhost"}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	return cfg
}

type caseSpec struct {
	name         string
	auth         tls.ClientAuthType
	useNilCAs    bool
	clientCert   string // "none" | "good" | "expired"
	wantServerErr bool
	retryCheck   bool
}

func runCase(cs caseSpec) bool {
	var cas *x509.CertPool
	if !cs.useNilCAs {
		cas = caPool()
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverConfig(cs.auth, cas))
	if err != nil {
		fmt.Printf("FAIL %s: listen: %v\n", cs.name, err)
		return false
	}
	srvRes := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			srvRes <- err
			return
		}
		defer conn.Close()
		tc := conn.(*tls.Conn)
		err = tc.Handshake()
		if cs.retryCheck && err != nil {
			if err2 := tc.Handshake(); err2 == nil {
				srvRes <- fmt.Errorf("retry succeeded after failed verification")
				return
			}
		}
		if err != nil {
			srvRes <- err
			return
		}
		buf := make([]byte, 64)
		n, rerr := tc.Read(buf)
		if rerr != nil {
			srvRes <- rerr
			return
		}
		if _, werr := tc.Write(buf[:n]); werr != nil {
			srvRes <- werr
			return
		}
		srvRes <- nil
	}()
	var cc *tls.Certificate
	switch cs.clientCert {
	case "good":
		c := mustPair(cliPEM, cliKeyPEM)
		cc = &c
	case "expired":
		c := mustPair(expPEM, expKeyPEM)
		cc = &c
	}
	conn, derr := tls.Dial("tcp", ln.Addr().String(), clientConfig(cc))
	clientErr := derr
	if derr == nil {
		if _, werr := conn.Write([]byte("ping")); werr != nil {
			clientErr = werr
		} else {
			buf := make([]byte, 64)
			n, rerr := conn.Read(buf)
			if rerr != nil || string(buf[:n]) != "ping" {
				clientErr = fmt.Errorf("echo mismatch: %v %q", rerr, buf[:n])
			}
		}
		conn.Close()
	}
	serr := <-srvRes
	ln.Close()
	if cs.wantServerErr {
		if serr == nil {
			fmt.Printf("FAIL %s: server accepted (want error)\n", cs.name)
			return false
		}
		fmt.Printf("PASS %s: server rejected as expected (%v); client err=%v\n", cs.name, serr, clientErr)
		return true
	}
	if serr != nil {
		fmt.Printf("FAIL %s: server error: %v (client err=%v)\n", cs.name, serr, clientErr)
		return false
	}
	if clientErr != nil {
		fmt.Printf("FAIL %s: client error: %v\n", cs.name, clientErr)
		return false
	}
	fmt.Printf("PASS %s\n", cs.name)
	return true
}

func main() {
	cases := []caseSpec{
		{"no-cert/NoClientCert", tls.NoClientCert, false, "none", false, false},
		{"no-cert/RequestClientCert", tls.RequestClientCert, false, "none", false, false},
		{"expired-cert/RequestClientCert", tls.RequestClientCert, false, "expired", false, false},
		{"no-cert/RequireAnyClientCert", tls.RequireAnyClientCert, false, "none", true, false},
		{"expired-cert/RequireAnyClientCert", tls.RequireAnyClientCert, false, "expired", false, false},
		{"no-cert/VerifyClientCertIfGiven", tls.VerifyClientCertIfGiven, false, "none", false, false},
		{"good-cert/VerifyClientCertIfGiven", tls.VerifyClientCertIfGiven, false, "good", false, false},
		{"expired-cert/VerifyClientCertIfGiven+retry", tls.VerifyClientCertIfGiven, false, "expired", true, true},
		{"no-cert/RequireAndVerifyClientCert", tls.RequireAndVerifyClientCert, false, "none", true, false},
		{"good-cert/RequireAndVerifyClientCert", tls.RequireAndVerifyClientCert, false, "good", false, false},
		{"expired-cert/RequireAndVerifyClientCert", tls.RequireAndVerifyClientCert, false, "expired", true, false},
		{"good-cert/RequireAndVerifyClientCert-nilCAs", tls.RequireAndVerifyClientCert, true, "good", true, false},
	}
	passed := 0
	for _, cs := range cases {
		if runCase(cs) {
			passed++
		}
	}
	fmt.Printf("clientauth: %d/%d passed\n", passed, len(cases))
	if passed != len(cases) {
		os.Exit(1)
	}
}
