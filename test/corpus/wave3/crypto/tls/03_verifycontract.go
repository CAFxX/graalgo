package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"strings"
)


const vcCAPEM = `-----BEGIN CERTIFICATE-----
MIIC8TCCAdmgAwIBAgIBATANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDEw9ncmFh
bGdvLXRlc3QtY2EwHhcNMjUwMTAxMDAwMDAwWhcNMzYwMTAxMDAwMDAwWjAaMRgw
FgYDVQQDEw9ncmFhbGdvLXRlc3QtY2EwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAw
ggEKAoIBAQCzxzhTxM0+w9UeyyNbzT/OlxMgOXJJN0DFqi4hoRtqheU0WtafiEhn
mm9w0m+ui9mxojAIOyEExeaKpauKqQj2X+R+dJIcQNvWnA/ez2zWIHv2YEtCuwei
CJn+y/baLpmR1zK86pLtkdRxZiv+lZEhI9C2iwjas16RAYfDbqFff29vFHzOHvRk
QXwW88XF1ydJxAPEurAdBdTMQJEFPXYsakG8wKrHs7gM9FIV9VC14igLPl4rAirc
SbH1w81vDbkz44ZUlfubWkgzCO1hjmwsQlzpnocGVSl/af0rniMvl0vtFEcIu21H
HLygcV9morN8G9tAQEI3gLtil9DFqnQZAgMBAAGjQjBAMA4GA1UdDwEB/wQEAwIB
BjAPBgNVHRMBAf8EBTADAQH/MB0GA1UdDgQWBBRsTdBJji7QGIVU2qPBarjOGLaO
8TANBgkqhkiG9w0BAQsFAAOCAQEAMUZCHR4ZOfrRRoMrV1+8rAFoZ4jZEPVN0efM
qsGbKKZ4Y/4uKgbCyrpNKya1AWRr6PFoJB2jYf++xNUPbyXvCo73JAsw/ssW039d
QR3+C0dB48RsGO87PiDE40HuGkl3cBvrF7PImPXymcmOEqsrwh9suybvrV4G68BZ
BOuLhb33ci+ma2k0tDt4eB5KxegkXDHwTB1TYnQMPXv51iIZ8FksD0zcfdW1oYD/
6ehrR13q99EGLenI7JyR0o1ORFJAioKJhylsKyxTRqfnevZzGYQuAf+pScleZoGu
GQT7wpzVBQX/1qXT+MNA9mrZ2LjTB45LweYziwvYkUuKh4nBWw==
-----END CERTIFICATE-----
`

const vcInterPEM = `-----BEGIN CERTIFICATE-----
MIIDHDCCAgSgAwIBAgIBAjANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDEw9ncmFh
bGdvLXRlc3QtY2EwHhcNMjUwMTAxMDAwMDAwWhcNMzYwMTAxMDAwMDAwWjAkMSIw
IAYDVQQDExlncmFhbGdvLXRlc3QtaW50ZXJtZWRpYXRlMIIBIjANBgkqhkiG9w0B
AQEFAAOCAQ8AMIIBCgKCAQEAoxxiGhbq/Twtodumaf/i4p0PtbZ2kfeMji2rbfmb
dJgpMORtyKhgJjHBq5+o7BWjBh1JKdmHSdHlnwwZl9l5dzzk42Yxnip9vPOWR9gv
Wshe5NbcYWNscUjYyQ/Hgw2XBo+xHG6FlP+8kYpMYSwpyV/JbVJS8L+AsJe0IlvH
zusdnZrbzJfu5azqLIeiwavz+mW3GrHDluXUnPeIoTlCj1YptEXbn5aj/tVpYdt0
jbqMkCnhwoGdMV96Zoy+8r4KCoQdYYSdIcTbHnCcUyZm6uZH0y0Z+pPqTIrDJ91d
VijjcHr6tbzvtNzlDl1EaZVgYXy0PbOQ1jRioCHwmYnCGQIDAQABo2MwYTAOBgNV
HQ8BAf8EBAMCAQYwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQUtltN9sqWjmIH
5mSQMcMQt74xzy4wHwYDVR0jBBgwFoAUbE3QSY4u0BiFVNqjwWq4zhi2jvEwDQYJ
KoZIhvcNAQELBQADggEBABFJCeQIqv0SmzXTUO4+DOOSR1/a/+4oCI2tuufAnML9
TEt6qdiqA2PlLl3244sJJSOZxlYP/ryXfLaGamgZEjmxjI8JGBukQ5pIVj6m2YCV
LZoS0k4txMbmyB0aQcwZWbBo7R4hXlvnqd8h4lZpt/WMVRkbRmS5Eq03P8u3Xecz
n/MlecurX7L5FXkow4HCGClb/iVGhfPCHxd0GjCxHzUufZzID6ojNjYkiT0/i+bS
Cc/uhFMTnfW3w4qtAGJzUJkG/xtSNG/BtPlixDrLVGlz9KVR45JIZyvN6M9W1QPt
nnM0qf5/lweZQBow9guzCV8M9KxR9/WIdbD2CQh+ZIo=
-----END CERTIFICATE-----
`

const vcLeafDirectPEM = `-----BEGIN CERTIFICATE-----
MIIDKDCCAhCgAwIBAgIBAzANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDEw9ncmFh
bGdvLXRlc3QtY2EwHhcNMjUwMTAxMDAwMDAwWhcNMzYwMTAxMDAwMDAwWjAUMRIw
EAYDVQQDEwlsb2NhbGhvc3QwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIB
AQDZ7Jt0KwktxBkivXDG/ynZ/Oxmr9zFGITYNPQ0Mmt9608mDzbXF1doffUszGOY
R4HJ/odpCJsKK9pUn31qz9aBlSWEaG63fm5SOJ+DCBvToHjnSsNgA1Cpby960bgs
vt1xBF1I0vl+P+Crxe+19VY1td1XYBZ9BnqaO0utixMZkd/aw7hOohq2Nd+kZftV
law3wkYHqok15eb+X1IyhDaI/VK6mZ6pF2N3nIHPvcz3SU31k/AWIwEGsS86mbZa
GGA+i+JelF4HzcYasYgwskVOXxWJv2ulyh+mdCCykhoUJPAO8qQu/ZbmEJdZGXfV
rwzGAnubpdEC0k6HH5DXnppxAgMBAAGjfzB9MA4GA1UdDwEB/wQEAwIFoDATBgNV
HSUEDDAKBggrBgEFBQcDATAMBgNVHRMBAf8EAjAAMB8GA1UdIwQYMBaAFGxN0EmO
LtAYhVTao8FquM4Yto7xMCcGA1UdEQQgMB6CCWxvY2FsaG9zdIILZXhhbXBsZS5j
b22HBH8AAAEwDQYJKoZIhvcNAQELBQADggEBAFR8FqrHEsKvLOIgc/XZ0kLRpXoo
iybH0XTMYLtzzLgoCHQtUnOQ5RdLorxWstjSYk1GOXIqbqfDcyHcZ96v7/mYdbUu
ap4cpucFXNevsCjTvJvYN+cdcUXBu9Bw2XAEGwhvvb2hY5wOaWrTQwSK7rOzgP/U
MH2lq/RK+HTJc1g9pg5JfPVWZmOXdl9P7bjjV5crwKHvv88cMFsN8iohvEptCFTE
fSa7qW75Fan9lk31z8MaSdYnA7fdJr+dyRwmPwbk2Rj+CnoVtPuka6qyXnDnumRa
PgRQ0HrPhE5bd9az5RwY8wm27h72lhp83koEDwB3SkAfqLHnXtid26Lr/Lg=
-----END CERTIFICATE-----
`

const vcLeafDirectKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEpQIBAAKCAQEA2eybdCsJLcQZIr1wxv8p2fzsZq/cxRiE2DT0NDJrfetPJg82
1xdXaH31LMxjmEeByf6HaQibCivaVJ99as/WgZUlhGhut35uUjifgwgb06B450rD
YANQqW8vetG4LL7dcQRdSNL5fj/gq8XvtfVWNbXdV2AWfQZ6mjtLrYsTGZHf2sO4
TqIatjXfpGX7VZWsN8JGB6qJNeXm/l9SMoQ2iP1SupmeqRdjd5yBz73M90lN9ZPw
FiMBBrEvOpm2WhhgPoviXpReB83GGrGIMLJFTl8Vib9rpcofpnQgspIaFCTwDvKk
Lv2W5hCXWRl31a8MxgJ7m6XRAtJOhx+Q156acQIDAQABAoIBAAYrwaWP4Z1B9Bnc
Vjw174Z0hHE0wg8NIbEWcGMMZnTU9ZNoIBWfjvvh5tAGim9xq7nMX/ALRiPXy0GI
Mdhh23rx5HN6Zy0Bz7fuUiRqVcQvw0/kkxTxIxb/k5wDu+Mwe6H+pwsv39BbMk5+
HWQzduYW3BZKt0Ubd0KBmVc06ZNYu75kHnSsA1yEb7wOE6o+kZ04LcLtawJSU94T
KK0IfbaI6jU5pNM6bWIQfjSZiOXSWetzuwxD9XzYRDjm+iTc5jIiaiBqvLHQJ9VY
XTIwfBPI4siyf8kLVDjwDmSPKFTd583zf/1g+2lfm7TLl8I23ysEdxCK2FGD1HOZ
z7qRUq0CgYEA4+7nuDkzNqSYfotUsl9aSIXXH1T+z2Mb3GGLhMK3ai6GcdgQj4gI
i6dyNsGtatWNs8+t5i9yOX0AruObUt8GSG4COHTZ9/s5Xi81t0uC2HOW0ZT4u53n
v0BbQN/5SUQnJyQ+vpzCt6ZYX7YXmKJlpmxra13k5LIB/HFAd2+Xm3cCgYEA9MIw
6dINBwitGRPaAeAZzinLZg49/CDnJkjCbZfPEWfQTYoTzFNmqGb8GWjCBEB13WHl
6h5yhGKhJwm3bD+jO0omQGFKdDWR/8/eA5U/jkFrS1MlsYA2Z2zDTWCM1FcsWHSb
S5zbDmCaaPc5Mc0lurujMP0Rx1L5DAEy0ITEo1cCgYEA3/t3h2fmNK8gD1I5o6Lh
5FxpXie20fLRgQn2ATsa4+PeR+cLtw/1Xc7pBHfkQxShz+2oNIexVl7JGhMeIhx5
XxmDYxkozG/6GtyHi5xPcwZ7UW5z0mJERoMmaSTvIhkv5F3Qrpz7F18Gft9AZsRG
4XivQm2dLULjgr+Dmxp0v9cCgYEA3g44VkpNq24BrVjHVyA8CUD92e9d6Rro22Dz
lH5ATOhEobR91chswKf+XAcgzr60sE8bSlapStY4orQ8ByeNLkNNhOc/p0La3J14
LQQBD0zmzQX4kpthwF96qP7JhfQ4tV/FbMo4K0kVjdmNuFMIwetuWQmgUZhDftKv
iWKLDC0CgYEAy+l7ZPpjRJG/wevlB04DJn71lCqWzchMjFx/rjmtjsOPnjTEWJGw
781Xy416WY49JTdjJRcNHn1g0PFjko0B7mulSdFe0BksBrc27wIYtadZadUEvxkR
FRcVB0ijBN6++jNIga7K3TtzPPNowhgXn4nRoLYnaY5+Lf6M5krCFk4=
-----END RSA PRIVATE KEY-----
`

const vcLeafInterPEM = `-----BEGIN CERTIFICATE-----
MIIDMjCCAhqgAwIBAgIBBDANBgkqhkiG9w0BAQsFADAkMSIwIAYDVQQDExlncmFh
bGdvLXRlc3QtaW50ZXJtZWRpYXRlMB4XDTI1MDEwMTAwMDAwMFoXDTM2MDEwMTAw
MDAwMFowFDESMBAGA1UEAxMJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEFAAOC
AQ8AMIIBCgKCAQEArqDs8Qn5Q0ucLKp60BUsru3Y7vmAAxNpF/N09Tlu9SLCcLu5
17KKtpsGHywqlIAl3nq0zbLXmab29ISKbHEmGTH0rokgwlUisoRMzNvRvdUA2Fcm
JDnXJuM6whl3VKzAAiaNQjK5wum5eq72+sQ4+1QhxfG5iIoQUYGfezJ3c0RIcnQI
yBZB/7qphI9RMbfdjeIqRgss83MRP3H5TpJzX16dXK9KbBSWm+WQobG99nnuxK/5
MaLNiVvAFdlBPpM/pmLqR5DJvuMV+aspnZ6LegaQGd6qojTsOTMqtS5YtgbHmDrU
5z4bnGHKrKfE8gKpEwZwzf3BUsgVffrpD3QbYQIDAQABo38wfTAOBgNVHQ8BAf8E
BAMCBaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDAYDVR0TAQH/BAIwADAfBgNVHSME
GDAWgBS2W032ypaOYgfmZJAxwxC3vjHPLjAnBgNVHREEIDAegglsb2NhbGhvc3SC
C2V4YW1wbGUuY29thwR/AAABMA0GCSqGSIb3DQEBCwUAA4IBAQCNLWnkSH6+aLvw
BADLK9OQfbM+wQQXS3zrdDY9m6Jq9IDApvddzN/e+xRdBJU6/JD4w8HV+9IlkFWJ
fZ6dRJmF9tOfSMWfBi4sUPP4C02Bh0ZJIwGvw6OPTbACTUAs2VWU90kzSyfCdnNp
Vl9m/SHrMbm/AEFyOILE7lYwpJ6TXzj4Qe859CYCfNmGKs6N9l4V0ErY5Vd6KvG4
c2QicCmCTyeHbveWtSpq0G5H+JXXGP7RDN2kXhcAFIdGnOP9bQe8h8n3RfzpYIpR
mmGdQMa4WvFd5geCbkHCNrE03BwUzAVh6jAIe4fBtM6/ZH6oCRVU3t63tYqIn8Eo
9I3ExuMS
-----END CERTIFICATE-----
`

const vcLeafInterKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEArqDs8Qn5Q0ucLKp60BUsru3Y7vmAAxNpF/N09Tlu9SLCcLu5
17KKtpsGHywqlIAl3nq0zbLXmab29ISKbHEmGTH0rokgwlUisoRMzNvRvdUA2Fcm
JDnXJuM6whl3VKzAAiaNQjK5wum5eq72+sQ4+1QhxfG5iIoQUYGfezJ3c0RIcnQI
yBZB/7qphI9RMbfdjeIqRgss83MRP3H5TpJzX16dXK9KbBSWm+WQobG99nnuxK/5
MaLNiVvAFdlBPpM/pmLqR5DJvuMV+aspnZ6LegaQGd6qojTsOTMqtS5YtgbHmDrU
5z4bnGHKrKfE8gKpEwZwzf3BUsgVffrpD3QbYQIDAQABAoIBAC5nrj9Jdrc21vwZ
m4RDNDoSE4VBwQ5QMKvAMvLdfcpfn8sEX9jdZbYr0P8kpfYLbcTG19gXr+pxb4u/
0ucJVGX6m/prj0yFAjDtLll8CqsG+PAEjKqwwidf1+Pvj4HtXCLWKtCOGUYeBP+A
kjpGPZwwGb93wmVxxu9Z4k8DNLHo4XVbaeZeAnhTzrB189lkiYGQazzWx86TvjGM
eqwNAPgLUpZdepDGWFLIBZnWUm2nNK1FeDNP1Lbzktkd6o5Gun7MnDJyvtzMUaNh
FCjZldCZpE1wOO8XV/ipSQsL0B8gYCzT2vjes6Cvm1W9knN8VWWUOB4qpAF1GvAV
C5HsLB8CgYEA0ft5LT3VOvK9mF9xKgWnweUWFiCvBPBffYD3I+l8aEcpx07BA6Ga
LArnfNIK/WMlewYbOKyruHxa7JYsLImjUCfJrXpt4NEtJiAiOnZXjzQ602duVEkV
eSuEZZlS1M3/t8/sr6ZL8wXEFysPK22GKPxZ00SqCrvT/c7G34Jf9hcCgYEA1OYF
rhW9RyLE6hlbJs6Rb6iQJsu+cVP/K5Bmy3rF0BrEZbmc+W5KkVtMHJa+QqFQhcJT
yaxO3DX5VvYbQkIDEgI3gBDKuQPFpPH94/17nWCBbMoCWYxLlXrO8btVwvfjcLGO
qE3hBNc3k2W2YM5J4Heq5IPPlXSXdwA4V4/j3UcCgYBmLSnv29CwWIHtbTPRsOLK
tMCkj87aZmTw2xpnPSwuScKIm0fMjoxIth2iXUq/tcj9LiF82HjO0qQjz4c7PNZd
IJ9AbPyekuqXTLebHw7IPjAJwqCrHsz2IzjPf2YQ6xRtyx1u80qtvLaIV4mQJkFM
HE9RQG5FvtQPAtk+1WAAjQKBgHCiAP9IoQqxalwvbfnp7yeTli/wOqDaP3VcwAWG
voCKqLzophOQNgMQugOl/KJjlaYuWQWpE0+NfoxzR5iWM+mjIpmuSJH5XOcW36tu
v6dTFyXm+bdGcfZzMmjRI14ypzFHvqFsEk/tTIm+le5BNPR8Wvzqfzgipr9ebnBk
v7ihAoGAa+aaOSjM/avQN9cj8QY+sGzmASn+2EoPf5XEl8Sj6BjCOQYiAh/eIYkW
RKbJEY0y1nv/4TQLla/Cagp4kHYh9yvYxO7nqS53RudwVniLJPfczvC7uodHSPaI
Bw+EwBgTO7kpWIpv9Cnej3jaVFW5LLYh8f8QnhPcnieXklgKko4=
-----END RSA PRIVATE KEY-----
`

const vcLeafWrongEKUPEM = `-----BEGIN CERTIFICATE-----
MIIDKDCCAhCgAwIBAgIBBTANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDEw9ncmFh
bGdvLXRlc3QtY2EwHhcNMjUwMTAxMDAwMDAwWhcNMzYwMTAxMDAwMDAwWjAUMRIw
EAYDVQQDEwlsb2NhbGhvc3QwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIB
AQDh3TCs5rjgXHowDX6ypsgKEamNo8iGAhixqgyby7HtQCC3rFVV0SENBu5XYUcS
EiMA0Sfycod9jFx60lRKbTmjAlTffQN6jROH/U/VjtURKEGYQDwAyh0dgWxeARt7
pGAT7qS+b1C9f5kyHZQuyA7APuwJXkoaBAwwewZbX6tLs/F6Js2+JbauB3C+WtvJ
M5gvt7TjC282VviD8CvNxfoSRSkv/aeEToN4ptncOQiKFXP5JbY0oXboKRbwAss2
n9Shi4ds+0Y42TdXDcRfEDpT1JE5vExvMippqSeghbL8xQEY+DdGbg4KvuuBBjhb
gQlDffaqBE//HMDCgdquK8G5AgMBAAGjfzB9MA4GA1UdDwEB/wQEAwIFoDATBgNV
HSUEDDAKBggrBgEFBQcDAjAMBgNVHRMBAf8EAjAAMB8GA1UdIwQYMBaAFGxN0EmO
LtAYhVTao8FquM4Yto7xMCcGA1UdEQQgMB6CCWxvY2FsaG9zdIILZXhhbXBsZS5j
b22HBH8AAAEwDQYJKoZIhvcNAQELBQADggEBAF1thOJJSfQcQ0bxKq6Zmfmcm/6w
8wG5RzUm8LzcUqpLLURjmzj0pf6PqnIHklzqmdISBVR0rACuVXF665RV5Fwu9pij
Uj56mYBpMOG/l8AELOw9NzmbL65iLEJmy3hHf9rCgXjvtFaDGecFjHC9vv3pPfP1
s+AuwavvyxdInJih3UI6wq05Qb71GfNL+uVGX+Lk+/efCnPsdVPu9g7UiXy0chD0
EFk31+p0/bAsfI2FA7LwS4SrkpE2fZnoTmmfGPk9GC4hXZ1OHjxwIb8jasUFq+yw
DzzqwSSiun3rePhYFvDVe3o9FBhO+6qEfLjC/qI1yCDEixpElAKQi92qfQw=
-----END CERTIFICATE-----
`

const vcLeafWrongEKUKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA4d0wrOa44Fx6MA1+sqbIChGpjaPIhgIYsaoMm8ux7UAgt6xV
VdEhDQbuV2FHEhIjANEn8nKHfYxcetJUSm05owJU330Deo0Th/1P1Y7VEShBmEA8
AModHYFsXgEbe6RgE+6kvm9QvX+ZMh2ULsgOwD7sCV5KGgQMMHsGW1+rS7PxeibN
viW2rgdwvlrbyTOYL7e04wtvNlb4g/ArzcX6EkUpL/2nhE6DeKbZ3DkIihVz+SW2
NKF26CkW8ALLNp/UoYuHbPtGONk3Vw3EXxA6U9SRObxMbzIqaaknoIWy/MUBGPg3
Rm4OCr7rgQY4W4EJQ332qgRP/xzAwoHarivBuQIDAQABAoIBAFLLKZYfx0JbM+Ec
eUaUs3xZaB1HHfDdoN6zdoa/ntvYp4WcsvVPwT1Mvp6iRpKdul4IzYtmpzsC8j2F
jydvLXMPHjyET3ujbjbpyNOAVaNB7fWZCkXB0PVpZWd6IXBr21pDHvm9xUuJlAcW
Z6TUae/XwN9YRHKP7EXzOuvQwHJxdfKGt3MdSZG8+KCVVbaF32Lfw77L+yM3EMWO
SPAbIY7ItZdoc6e3BiJ8sGp2/HddArN5JLXcIEkOBvh8inmJGuydMdAZYfK3NX+i
M3ChwojmKqsEhk6+0UktUqkYiOt2U9GIbgWG12qsk7SCAjeC/Ot97Sn35OGW9sRF
eFcEiecCgYEA7QQpTEWVglFkSZN5pZzRTeTuGNpRDZBqlI16BcqHUJg8p2oAVhje
r8C/+IehP0H1S+YQeutXTW3lwLvPaI7h3OZ9jCNP7h8MOEW9o2Y+nfuq+bw0pP/8
0UInqflfYMXnOmjzAdbkwMH5OTGQXWYrz7Rw6eKDdSpXq38jCJYLBQ8CgYEA8/Rc
W2y3ZBBwgrxldSc5tPgT3GGPstdZpMXMmkHhoaEYnB1GNhnD+xvoPAz2d7W7BH5Z
qJvJYp6lOYBOsp0MNMiBwG4mGnrmEBe3HB6sRP5ZfwLSQpuK1QyPdSJWx7s1cSQN
rK3suaL87Hn+EPKqqtA9+0895z7ty/Y2H5NqnLcCgYEAww6sqh+OFozDWyXqdBhl
zJQ+OufY0q/qds0kAgYWbIaNTD4ZfsHC6qNXmcKNOpEevdLd9zsdQxTRg1dNL0B/
sxthcWZSTqbwmJMUqZfoqn3GzPZN4QjnWJP4LpOTIgJc9J4Qd5AR/GzmnWIj7Cm8
nphiYKlRBKjPM81S2Ksa8JMCgYBaWZYbBhilprEEumGZEf39WCOC618dDUTWEavF
NcEq608CApWOU9yRk7RfCS3/B/9Lmr/6AjiDnd3zfeGnpSJwKhJkAmd6RyoNq7rV
X0bOBqV+tLzKjnDMlupKp30cW+lfDctCuOeDmSmzKzs9a6JpwNYzzRVQH65Sa81/
xEkn4QKBgQC06zsMsarmdqgwKQtloGTd6cLQ8ysK/wcHOawi5KY2VRNyMnsQqUGT
z4KihJLik8NS1aWq9DyC32QCFt4P5/sIutYU7OakVrlcBwPnyHlwHWkdjGd6kXJX
3SL2pMIPvZjJ0C0h96xIgnhC0R5xp0MUF0q5tzGWJ8+cZlcgsiXYcg==
-----END RSA PRIVATE KEY-----
`


func parseCertPEM(s string) *x509.Certificate {
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		panic("no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		panic("parse cert: " + err.Error())
	}
	return cert
}

func rootsPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(parseCertPEM(vcCAPEM))
	return pool
}

func mustKeyPair(certPEM, keyPEM string) tls.Certificate {
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		panic("X509KeyPair: " + err.Error())
	}
	return cert
}

// interChainCert presents leaf + intermediate: the server chain the
// pre-fix client could never verify (intermediates were dropped).
func interChainCert() tls.Certificate {
	cert, err := tls.X509KeyPair([]byte(vcLeafInterPEM+vcInterPEM), []byte(vcLeafInterKeyPEM))
	if err != nil {
		panic("X509KeyPair chain: " + err.Error())
	}
	return cert
}

func serverConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{cert}}
}

func clientConfig() *tls.Config {
	return &tls.Config{RootCAs: rootsPool(), ServerName: "localhost"}
}

// runHandshake runs one in-process client/server handshake and returns
// the server handshake error, the client ConnectionState (nil when the
// client handshake failed), and the client handshake error.
func runHandshake(serverCfg, clientCfg *tls.Config) (error, *tls.ConnectionState, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("listen: " + err.Error())
	}
	port := fmt.Sprint(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	tlsLn, err := tls.Listen("tcp", "127.0.0.1:"+port, serverCfg)
	if err != nil {
		panic("tls listen: " + err.Error())
	}
	defer tlsLn.Close()
	srvErrCh := make(chan error, 1)
	go func() {
		conn, err := tlsLn.Accept()
		if err != nil {
			srvErrCh <- err
			return
		}
		defer conn.Close()
		srvErrCh <- conn.(*tls.Conn).Handshake()
	}()
	conn, err := tls.Dial("tcp", "127.0.0.1:"+port, clientCfg)
	var cs *tls.ConnectionState
	if err == nil {
		state := conn.ConnectionState()
		cs = &state
		conn.Close()
	}
	return <-srvErrCh, cs, err
}

// runServerEmptyConfig drives a server with an empty Config against a
// real ClientHello and returns the server handshake error string.
func runServerEmptyConfig() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("listen: " + err.Error())
	}
	port := fmt.Sprint(ln.Addr().(*net.TCPAddr).Port)
	errCh := make(chan error, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer raw.Close()
		s := tls.Server(raw, &tls.Config{})
		errCh <- s.Handshake()
	}()
	raw, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		panic("dial: " + err.Error())
	}
	defer raw.Close()
	cl := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, ServerName: "localhost"})
	_ = cl.Handshake()
	ln.Close()
	if err := <-errCh; err != nil {
		return err.Error()
	}
	return "<nil>"
}

func main() {
	passed := 0
	total := 0
	check := func(name string, ok bool) {
		total++
		if ok {
			passed++
			fmt.Println("case:", name, "PASS")
		} else {
			fmt.Println("case:", name, "FAIL")
		}
	}

	// 1. Direct CA-signed leaf: verified chains populated, length 2.
	_, cs, err := runHandshake(serverConfig(mustKeyPair(vcLeafDirectPEM, vcLeafDirectKeyPEM)), clientConfig())
	check("direct-chain", err == nil && cs != nil && len(cs.VerifiedChains) == 1 && len(cs.VerifiedChains[0]) == 2 && len(cs.PeerCertificates) == 1)

	// 2. Leaf via intermediate: chain length 3, both peer certs presented.
	_, cs, err = runHandshake(serverConfig(interChainCert()), clientConfig())
	check("intermediate-chain", err == nil && cs != nil && len(cs.VerifiedChains) == 1 && len(cs.VerifiedChains[0]) == 3 && len(cs.PeerCertificates) == 2)

	// 3. Wrong EKU (clientAuth-only leaf as server cert) must be rejected.
	_, _, err = runHandshake(serverConfig(mustKeyPair(vcLeafWrongEKUPEM, vcLeafWrongEKUKeyPEM)), clientConfig())
	check("wrong-eku-rejected", err != nil)

	// 4. VerifyPeerCertificate pin rejection, with InsecureSkipVerify:
	// the callback must still run (gc calls it even when chain
	// verification is skipped).
	pinCfg := &tls.Config{InsecureSkipVerify: true, ServerName: "localhost",
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return fmt.Errorf("pin-reject")
		}}
	_, _, err = runHandshake(serverConfig(mustKeyPair(vcLeafDirectPEM, vcLeafDirectKeyPEM)), pinCfg)
	check("pin-reject", err != nil)

	// 5. VerifyPeerCertificate pin acceptance: the callback sees the
	// presented leaf (matched by serial) and verifiedChains is empty
	// when verification was skipped.
	expectedLeaf := parseCertPEM(vcLeafDirectPEM)
	seenChains := -1
	pinOKCfg := &tls.Config{InsecureSkipVerify: true, ServerName: "localhost",
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			seenChains = len(verifiedChains)
			if len(rawCerts) == 0 {
				return fmt.Errorf("no raw certs")
			}
			got, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}
			if got.SerialNumber.Cmp(expectedLeaf.SerialNumber) != 0 || !bytes.Equal(got.RawSubject, expectedLeaf.RawSubject) {
				return fmt.Errorf("pin mismatch")
			}
			return nil
		}}
	_, _, err = runHandshake(serverConfig(mustKeyPair(vcLeafDirectPEM, vcLeafDirectKeyPEM)), pinOKCfg)
	check("pin-accept", err == nil && seenChains == 0)

	// 6. VerifyConnection rejection must fail the handshake.
	vcRejectCfg := clientConfig()
	vcRejectCfg.VerifyConnection = func(cs tls.ConnectionState) error {
		return fmt.Errorf("verifyconn-reject")
	}
	_, _, err = runHandshake(serverConfig(mustKeyPair(vcLeafDirectPEM, vcLeafDirectKeyPEM)), vcRejectCfg)
	check("verifyconn-reject", err != nil)

	// 7. VerifyConnection acceptance sees populated verified chains.
	vcOKCfg := clientConfig()
	vcOKCfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.VerifiedChains) != 1 || len(cs.VerifiedChains[0]) != 2 {
			return fmt.Errorf("verified chains not populated in VerifyConnection")
		}
		return nil
	}
	_, _, err = runHandshake(serverConfig(mustKeyPair(vcLeafDirectPEM, vcLeafDirectKeyPEM)), vcOKCfg)
	check("verifyconn-chains", err == nil)

	// 8. SystemCertPool loads (non-empty, no error); a test-CA leaf
	// verified with nil Roots must still fail (test CA is not a
	// system root) — the negative control for the nil-Roots path.
	pool, poolErr := x509.SystemCertPool()
	sysOK := poolErr == nil && pool != nil && len(pool.Subjects()) > 0
	_, nilRootsErr := parseCertPEM(vcLeafDirectPEM).Verify(x509.VerifyOptions{DNSName: "localhost"})
	check("system-pool", sysOK && nilRootsErr != nil)

	// 9. Server with an empty Config fails loudly with the gc message.
	check("server-empty-config", strings.Contains(runServerEmptyConfig(), "no certificates configured"))

	fmt.Printf("verifycontract: %d/%d passed\n", passed, total)
	if passed != total {
		os.Exit(1)
	}
}
