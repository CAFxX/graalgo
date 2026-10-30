package graalgo.p3x509;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for the JDK-delegating X.509 intrinsics ({@link GoX509}).
 *
 * <p>Fixtures are real certificates generated for this test (openssl for
 * the valid/CA/EC/Ed25519/wrong-host certs, keytool with a 2020 start
 * date for the expired cert) and embedded as base64 DER below, so the
 * tests are hermetic: no keytool/openssl invocation at test time, no
 * network, no clock dependence except the expired cert's 2020 window
 * (expired long before any plausible run date) and the valid certs'
 * 2036 notAfter.
 *
 * <p>Where GoX509 has no verification entry point (there is no
 * {@code verify} intrinsic; chain/hostname verification lives on the
 * Go side), the tests pin the exact fields that verification consumes:
 * validity window, SAN list, CA flag, and key material. A test below
 * therefore proves the inputs to verification are correct, and the
 * gc-style validity/hostname checks are evaluated inline from those
 * pinned fields.
 *
 * <p>Fixtures are embedded as base64 DER literals by the authoring
 * step that generated them; they are not read from disk at test time.
 */
public final class GoX509Test {

    // DER fixtures (base64). Generated 2026-10-30:
    //   rsa:     RSA 2048, CN=example.com, SAN DNS:example.com, CA:FALSE
    //   ski:     RSA 2048, CN=ski.example, SKI+AKI extensions present
    //   ca:      RSA 2048, CN=Test CA, CA:TRUE
    //   ec:      P-256, CN=ec.example, SAN DNS:ec.example
    //   other:   RSA 2048, CN=other.example, SAN DNS:other.example
    //   ed:      Ed25519, CN=ed.example, SAN DNS:ed.example
    //   expired: RSA 2048, CN=expired.example, validity 2020-01-01..2020-12-31
    private static final String RSA_CERT_B64 = "MIIDHzCCAgegAwIBAgIUIXw6FTLop6MThqwSG4sycNFeWYUwDQYJKoZIhvcNAQELBQAwFjEUMBIGA1UEAwwLZXhhbXBsZS5jb20wHhcNMjYxMDMwMTg1MDM1WhcNMzYxMDI3MTg1MDM1WjAWMRQwEgYDVQQDDAtleGFtcGxlLmNvbTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAMCTFc3D7WfmJKpp0y3LaMCrl4DREPtNEHh+NX8RTFl1m+HezqR0cqksjtRUlx5Kl6mymyIBV5Yahap2Zo+4ljW5KBXM28En3UtvrkWZporP8eV+fuKlZ8L5gSvKTrd7bf0RpQ0AQBci9RSKP3yogrTDK/KFG00FgL+myf7Ylpav6q6OHcbOZM9kJO8qBI839gPzP0sxGBm1ao57neTcJx6HvuM5oPxkiI1oWMYlD58jdj5ECVHwmMHXb3Gwc+iRhKMR3QInyzH/WSXq3Icp/bSgxnCRNQydvUVxGiOQZOMNFb1HPLZhbsFhUCBFPXtSsTGOfzW36mrzUTlniCGZWAcCAwEAAaNlMGMwHQYDVR0OBBYEFB1wpX9Vp54vz5DbGTq1Reu7P2BOMB8GA1UdIwQYMBaAFB1wpX9Vp54vz5DbGTq1Reu7P2BOMBYGA1UdEQQPMA2CC2V4YW1wbGUuY29tMAkGA1UdEwQCMAAwDQYJKoZIhvcNAQELBQADggEBAINBWsOtSMLFo8x3a1u1xGI2HhVxlAfbIz+Vcj51Z1arjGz/HbP+iL1ymHZUsdHyDiETPTfd6d9qtdVDpOsWgnJF5ulhi3DQq50xVxWQ4sxifIBxS2Pt22P2VUq47VVHYKi+Um4QWLqW2oMp4BsQL0XP19rLUC6bTK2Di3pfQPLA0PC7meKayYZksGfVQAbQX4Oh1NWaEfQDsOx7hEY7mQ3FTfLhXoxJQQi3LTrqDjPdbmfriaAN/b0/8teqRtAtb+0wOKQzRVv3frBjs1VqKhR2pVVaAXGsA89FWXEX80mr8N7Sas7W+zJieYqbKtE0QLLYV6NhA4ol8j66cKsW3BY=";
    private static final String SKI_CERT_B64 = "MIIDHzCCAgegAwIBAgIUaCkJoi3KhVZgBsR1pqubs/0t5QAwDQYJKoZIhvcNAQELBQAwFjEUMBIGA1UEAwwLc2tpLmV4YW1wbGUwHhcNMjYxMDMwMTg1MDM2WhcNMzYxMDI3MTg1MDM2WjAWMRQwEgYDVQQDDAtza2kuZXhhbXBsZTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANVmnKo65D31/gBDwnA1YvF/P3XCc1flE3lmPNelRRhzqi8rrKcd1kVEgLi/mBi6RQ4Dk/BMQSh9sD2fOEpZYOUCzUiaKpSRiliPaMOl++mujpn50ZBUtKK+B6C++rYjSK9T3OCnMr3is6x1D2bziHChRxiZCHuUr8n9XA7cxbKmZmF8IcIbEpLe2hHsCdzHpzAixMb5V5t8Ig8fkxmvb/GwtgdUBuHs5VCEkooX5KOdwR9gE71dAxtWcSUqA0SKGvsVqqv5dBKK2ch+7H2/KTW0XssnATNjQEvM9Q0e1zOP6k8qvhrtzVzP/TanfzUhwK8o7qCOXjFS6w0euoH4v88CAwEAAaNlMGMwFgYDVR0RBA8wDYILc2tpLmV4YW1wbGUwCQYDVR0TBAIwADAdBgNVHQ4EFgQU/X/zZKIbCYca929SpiDG9gbCmlswHwYDVR0jBBgwFoAU/X/zZKIbCYca929SpiDG9gbCmlswDQYJKoZIhvcNAQELBQADggEBADxhhJxwBSw0qlNXI7qJDdX33vHutO5wzv3tSTsCFj8OxIEYb00LojJse63heElBiANQe3/RyWlYpET8rtVHCnT1ZDwQ4Z4q8V/EsHzH6ZrkeY9zq91MOd43Myn3DgyyYhpO128Dd76wNbMl5V8sZj82LYMZtsepvagON12G3EuYQTR9e/fsILgM4aoNMlqXrowiZxAcUajH6tnVfrnh5XPsMJktrYgvrlp/qtBlPu8GMyG5X0NXXoiyRwalnqRnM7ZQskPvNYGHKUHpEDIK8J6TKKuekojbZd1EU2EEhRJrfUXMQHneTicCRUxMj+WEcEPx8SlDkqUsg/n/qBy3IlQ=";
    private static final String CA_CERT_B64 = "MIIDBTCCAe2gAwIBAgIUDCLZyVUfxL1Bs1q/HHQhMW1zxdIwDQYJKoZIhvcNAQELBQAwEjEQMA4GA1UEAwwHVGVzdCBDQTAeFw0yNjEwMzAxODUwMzZaFw0zNjEwMjcxODUwMzZaMBIxEDAOBgNVBAMMB1Rlc3QgQ0EwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDQbgnkunqrNoCDbb7pX0mqNbSuzeuikZgeGH/F6ZXNPCD8wC/C8InWkhAOOW1tJ3SiukybLvNfcSY+RoSnOjtoeYfxIZRAjMHyZtHxR18tX9iNx0VojQ9fn8Yc8GT2QybQtcvUA+uv7NHrNxcDaOYY3H6/Odbiorb52XBiydk6TD/hMbf40iEuivFL0Gov+IDjhxYVF/15JKWqEfEJGmh2MnsPH1ldhJo2PfnZT17rNPVJce1EZ+DM4vShyt1pQb9X16H5Gorx1TweWJbJBooi4D04ZhEox7meNra2JVJNS23OAybnzJrE19Cr70Ha3unt1w/zD4albfe6E9ltszUpAgMBAAGjUzBRMB8GA1UdIwQYMBaAFP1XpGpBDmX9JPs8YFukp/c6WhKeMA8GA1UdEwEB/wQFMAMBAf8wHQYDVR0OBBYEFP1XpGpBDmX9JPs8YFukp/c6WhKeMA0GCSqGSIb3DQEBCwUAA4IBAQAyJsY6z3HSnh8T35CBDaymw0k0f/f48eZuTbCjRVGk2U0oBZYggBBVJ0qwEstN6D8P9sZS08KEe02EbGMayU++d+KKUKWgOdISFrLR17NA16StAwmxYkOjd3G5C8hHUpI0lUj0TEsBUSRvTvwX9Wx3ze+zxebAYag6UFMp0dIdZs+qmtA0U4+LKZDiftb0IOT8cIdg4Fi3jy5my3vRoYx8+xxHdp1YWmSHfd8Urme1npZAV4qDkzlXglC+1CeLrj2wixMDBiMGTnJcHAUwBb/LwlFlNDm8Dfvw6l/XV81cbpLH8SjKb8GT282+bM4+K4jL4+sdaVJuyC+zwkYaI9Sq";
    private static final String EC_CERT_B64 = "MIIBlzCCATygAwIBAgIUI1BmUCUZbn3farSbmQxaWa9HZJMwCgYIKoZIzj0EAwIwFTETMBEGA1UEAwwKZWMuZXhhbXBsZTAeFw0yNjEwMzAxODUwMzZaFw0zNjEwMjcxODUwMzZaMBUxEzARBgNVBAMMCmVjLmV4YW1wbGUwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAASya6JbPQZaxtQUWPg+S4Wtb3GB819S7QNhNiPDT3GV7XzlMe+abdj0+lbJfqnCFAPTpJNV77V5Vz8CS2JTDBSBo2owaDAdBgNVHQ4EFgQUetaq2QXJ70+ERjB3gT41U0QQNfwwHwYDVR0jBBgwFoAUetaq2QXJ70+ERjB3gT41U0QQNfwwDwYDVR0TAQH/BAUwAwEB/zAVBgNVHREEDjAMggplYy5leGFtcGxlMAoGCCqGSM49BAMCA0kAMEYCIQC08G3YzwnNpW3w7E1zrYmy+ldFk8/ZFX457Yj17w8T1QIhAP6PHxXeUK4d2fWJAwRmlwq5Gf7RKn4ABQQOxDOGPFWs";
    private static final String OTHER_CERT_B64 = "MIIDJTCCAg2gAwIBAgIUFH215E/uEKhREpWWXj6zvWGa3D4wDQYJKoZIhvcNAQELBQAwGDEWMBQGA1UEAwwNb3RoZXIuZXhhbXBsZTAeFw0yNjEwMzAxODUwMzdaFw0zNjEwMjcxODUwMzdaMBgxFjAUBgNVBAMMDW90aGVyLmV4YW1wbGUwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCdGY20tl7Vr3JAlgnKlCn6kCwm1I8R0XwtQfTnamOXEmXQZ3VLjnP9QCxUhWjBCRQ75ixahS3or4zU/gnq7880/Y7yS9wO8XZaom+sw8tg2HsHCbEN7lVIHY+Pt+WYfVq7SauX8JuM2QYXXDlI1PK3CS7qbd89D67+jmvn5+DDOsaVtaoIuoAw90NYC3qb42ooLz4t+GFNzRWP21evD/MFdlBLNH2n16aNNEpWudmJUHhzZ1IZn6eQjASZt0S/5iLvYt9QMdkqJdaZoUVN8tHOIldkklylxR43cuOAt+DTUOdxJAbCztpUuTAg7Ag80rOFENaIwByzjwwtAh3rvXBJAgMBAAGjZzBlMB0GA1UdDgQWBBRQ6qP5/5WOH6oDaKkW0McxaJ4BzTAfBgNVHSMEGDAWgBRQ6qP5/5WOH6oDaKkW0McxaJ4BzTAYBgNVHREEETAPgg1vdGhlci5leGFtcGxlMAkGA1UdEwQCMAAwDQYJKoZIhvcNAQELBQADggEBAFULMGuoZ7MBxr5r3LCAPkPH0/EwnUyqNozUVFSMSNxlPkpzg4PcyidrZXaMUx+691SCit96k5riwfm6oKdqqvlPnLE0cigDU4pkV2/agWrZ7Z1GIGqxfdZml6ojUsDcYJCHLjDOexdSAyUkuBGe81sJXJwBZzJ7YeDOQMQgqe8XXbCAmPkrpC1iTmDF31WrQQWTE0ghGCPm4ztd6meN+w7Gc3RBzkA5yLJ6g2SVxEIuuX/IzI5JinuLHiwHg1TDb85tUyr9S5OKwfn+bsJBahQ9etaxfTmHEF7T+8aXkdPUkMzA/64q1wjFsftTQpEIoF8btm1YkCUW1TePKKgwDQ8=";
    private static final String ED_CERT_B64 = "MIIBVjCCAQigAwIBAgIUNb1tVQqDuzmoziyjrt9+uD8G10QwBQYDK2VwMBUxEzARBgNVBAMMCmVkLmV4YW1wbGUwHhcNMjYxMDMwMTg1MDM3WhcNMzYxMDI3MTg1MDM3WjAVMRMwEQYDVQQDDAplZC5leGFtcGxlMCowBQYDK2VwAyEAIS7aYuvNCpxg0mglx266GkrsPLCtDs1cta+YPIIJd+6jajBoMB0GA1UdDgQWBBTuG633FF8WBjrP/HcZXLf/wM3MLDAfBgNVHSMEGDAWgBTuG633FF8WBjrP/HcZXLf/wM3MLDAPBgNVHRMBAf8EBTADAQH/MBUGA1UdEQQOMAyCCmVkLmV4YW1wbGUwBQYDK2VwA0EAS7s9nzM2rE15sq6kxCr2PVrDDqb8bjLDD7qoj5PtmFwuSBoSoEp1yDfX6icYKPTyoqj+XGQsr579PzWgJekiBQ==";
    private static final String EXPIRED_CERT_B64 = "MIIC2DCCAcCgAwIBAgIJAPS396rdyfUTMA0GCSqGSIb3DQEBDAUAMBoxGDAWBgNVBAMTD2V4cGlyZWQuZXhhbXBsZTAeFw0yMDAxMDExODUwNDBaFw0yMDEyMzExODUwNDBaMBoxGDAWBgNVBAMTD2V4cGlyZWQuZXhhbXBsZTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALw3f2d1U2PMVF+jR1hrpOOgSGfbZ9phFEE0JSB9AJ9woxiIE6oFi79gyEaIjGWOJ0HXdScCB/o4cpvxzHahY7RYTmF+5q9umtPY6VpqdSXagA1SJV+/fliH6UoKT48aAJ0ksI3Y3w82cwsTw27V3/8dYT1DTQKedPME2Scqlw7OhzGak/flactwHHuPLEDggik4YRQWNqaZFKz4LSAo6zh41pndASbn1mrYKxn5i7U4NMtkZwaZRnmwdoTrj2Oywszu5N3zlnq2/9P9PbHba/8InxRgayfiAfODHKmu+GZXc1v6AOGer6DxoMUxL3RLpCWcoxid51Pt5nqUUZPQBTMCAwEAAaMhMB8wHQYDVR0OBBYEFJTaMv9Nz/2tPR1CKgGraT9KPXC6MA0GCSqGSIb3DQEBDAUAA4IBAQCufSw/mP3vwR9TY9Q7khNWCoMu7cRprCSXQBkhscQSufYW98Zp4kOr/HtrsMoj7fbvO2Ys/EDoS/1oWFhgDHegpN9xZSCKNwbAxQeUfFb41onhuFlmVqw+9o4ZdKd6TiE1yhA7yzwpWIRLoa38Lsqm9jtIuB3iKqY8nDo4wZTJo24TTiJs0AzWP3rEGng6BSKEgk5R4O3qK5Niu+6Zb04gwbicRWUWEork/IfIf4M64ObTo/Xpvnyl+cjT3R8iz7JMpfFoEtq3q24Vss/AMuvlR+4LAR1Wb3GBnkkcpGRvzzUW+YUZh6OUh7QQm3DTnexsq0XTvD93e83eagnI7Kmi";
    // No X509v3 extensions at all (custom openssl config, no -addext).
    private static final String NOSKI_CERT_B64 = "MIICtzCCAZ8CFG5nrZwaRYIVlIxpdRJbmKVg+nTTMA0GCSqGSIb3DQEBCwUAMBgxFjAUBgNVBAMMDW5vc2tpLmV4YW1wbGUwHhcNMjYxMDMwMTkyNTQwWhcNMzYxMDI3MTkyNTQwWjAYMRYwFAYDVQQDDA1ub3NraS5leGFtcGxlMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAr+5on0jLdkpqrPxrUPIuZl0ukcjSfKPQLhJSPsT1hj0013JTQ9aLfXgw+gWon2IIQ7m5AORnySmoDgeMRaeIgVTxdoHXDQ9GkeVEMNkFGAY+l7T7i1ZC13Aejr4aRXJ1yYNLW6p09zx21t2PcH6Spe/zVZjjSrzSJR6LdjJdVjzx8BdZ6g3VLdzx0fKlwtnD6G8uPkvumP6AWAjF0JoI0d4J1WX80osXXGkGIAYPE809syY79GyNm/42kTIg+H0jhCVe7kV/xQttlGjpMdhMwlKxaXM3CmbxTFDFzl6PqoOHHYfIEOSkTK1RGnQcz1qFxsG9mYu0QtR3yxHAh4/kzQIDAQABMA0GCSqGSIb3DQEBCwUAA4IBAQBQl29Pesisvud9De8IdQG67hySNjfFwWW+0YdiKmTJUcjQKEL5G7ACBystfEL15cC3BbKvzQ7LU2P4BeL6j/04bv59g28OltAObrwwEuGgurS/eie86yqz2hoV0MTg3p5QwKXKi7AA3m+OdJd8qG4BIM3pgji3xJokyu/BBkGkVSh8LiFOVu85tGYqy/Uao+dPA7CiTmp4aat4E0hvqZwcxvCSS0X96gVeqsodWNuivDIdtQp5I0yjiGZrQqkpy6Nx2EOuQl4XTaWLRFkksBVJ+TwSU0Wn9hTGyNOhTDRT473hkiHkr8iP9lKR/IRvBCRp+ZRf9PIub/9cg+fiSpc2";
    // RSA public key material for the rsa fixture's key.
    private static final String RSA_PKCS1_B64 = "MIIBCgKCAQEAwJMVzcPtZ+YkqmnTLctowKuXgNEQ+00QeH41fxFMWXWb4d7OpHRyqSyO1FSXHkqXqbKbIgFXlhqFqnZmj7iWNbkoFczbwSfdS2+uRZmmis/x5X5+4qVnwvmBK8pOt3tt/RGlDQBAFyL1FIo/fKiCtMMr8oUbTQWAv6bJ/tiWlq/qro4dxs5kz2Qk7yoEjzf2A/M/SzEYGbVqjnud5NwnHoe+4zmg/GSIjWhYxiUPnyN2PkQJUfCYwddvcbBz6JGEoxHdAifLMf9ZJerchyn9tKDGcJE1DJ29RXEaI5Bk4w0VvUc8tmFuwWFQIEU9e1KxMY5/NbfqavNROWeIIZlYBwIDAQAB";
    private static final String RSA_PKCS8_B64 = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDAkxXNw+1n5iSqadMty2jAq5eA0RD7TRB4fjV/EUxZdZvh3s6kdHKpLI7UVJceSpepspsiAVeWGoWqdmaPuJY1uSgVzNvBJ91Lb65FmaaKz/Hlfn7ipWfC+YEryk63e239EaUNAEAXIvUUij98qIK0wyvyhRtNBYC/psn+2JaWr+qujh3GzmTPZCTvKgSPN/YD8z9LMRgZtWqOe53k3Cceh77jOaD8ZIiNaFjGJQ+fI3Y+RAlR8JjB129xsHPokYSjEd0CJ8sx/1kl6tyHKf20oMZwkTUMnb1FcRojkGTjDRW9Rzy2YW7BYVAgRT17UrExjn81t+pq81E5Z4ghmVgHAgMBAAECggEAUFAHU91WcXJl8DxaOksO0Ou/L5YBgwzMxbO9E9VfBiAqdrS5cZSvDSbsGll8/MEXJ0q5sCDnKk69WC4w79eHrbgeo5jgPVomzIMzJwvBjTrUI2Xx/QwGjiYIXP7ufU8Yu+ZT82wbTorhs0NE3vRYLpMHDQm+LQ0SeNtSetpRU14np4ZqYS6IJJTptvxNF1zcmUtIHkIsHOWB2DorJiX3+tjRZUSI07fB67rDDXqrLZ6tzvOijVNturwyqTLnTsswE/FnbpxYqEpvNscGkPa6pYv3qRgHupCxnfY6OIlzR1mB/NThtLDEK5BIahBSvnlfp7OLYMr3aBd9mCpyJ9gJwQKBgQD9lWrWWRiLNTiD3o1Cy4yE5dTSMD2J2xPX1qnPMo4IVmG7kjKfbmR5I9Wq0vq50WJbw8i1otIHeYnHvQhMQVRigzePKEOOkQ1F6scSrG13CEB85s/g1or18mOT2kzxmKW1E/aDlRqGgXHaV68/+W8M0OIB/Hf7N3nALeaMUiCFDwKBgQDCaNguSwvlL/JSw3PYGMW+Kdi6RO2wWMRHGNEjjWQ0bdmLS7oIaJEl0kgsKwKKeibm35/XZyFPsq/DdFB7QoJEacuWC+jjriRSFhpo22YPWA4KLKgum/mQE2adfR5S811coXVNjXQVDho1SKwzGAx/KiHDlIfUDC8lEsWPjUmtiQKBgHMG9A+HGrsKyTgIG8vQyUz/dCpj94Yw+i9OEHkMVaEyjeKnzo7DZJAO0T1Ltq/Taup1RPYfhWYem4EfHkR43e5R10H44awp3/t3JBY0YPM9qwL9MA6tQxuNd935iC2PMJuTEp9dY0rgLOT+IWDDpLwLhHmiLEqoP3INYCcGmfnBAoGAB1kJGq7Rc0cdpI0omrlM+AQC4f9q8GhaeslwViSo7eZc+4vQKzMwkTpiafDyNqOpRr6a/vwx4R5C0GNcC1yXKdjJzhEDK1/QlZ2qP95S/C960F2hualBhHWHQlqRQ9dY5wBfO6AmDAmtaegmQgQdrMy92/OIMraHLaKI/wT7DOkCgYEAos62suPiM8s8WrJxnGyI0C8lG21pvlh62T4C5ZEBSyDOGp4Yrzc+kF9bWk0NMv4rKGbkHDjW+LRf4HNqr+uIaOcziafcoLDh3BElNErQNd3Hy2gNYVUXTdhtTXmivveZvasPEK1urz9cFvqoTZvt0M7HoAJVHQ+jpm//QmKhL9E=";
    private static final String EC_SEC1_B64 = "MHcCAQEEIA0U6Mjn3bUWRyUN4BdCWkP0aqFAetyi0NfGgJLO3M7voAoGCCqGSM49AwEHoUQDQgAEsmuiWz0GWsbUFFj4PkuFrW9xgfNfUu0DYTYjw09xle185THvmm3Y9PpWyX6pwhQD06STVe+1eVc/AktiUwwUgQ==";
    private static final String ED_PKCS8_B64 = "MC4CAQAwBQYDK2VwBCIEIPZj4GYxB0ykOrp9n6EuNzAM/byB5TI9EPJQHAdPrWzr";

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    private static void assertEq(String want, String got, String msg) {
        if (!want.equals(got)) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    private static GoSlice der(String b64) {
        byte[] b = java.util.Base64.getDecoder().decode(b64);
        return new GoSlice(b, 0, b.length, b.length);
    }

    private static String str(Object o) {
        assertTrue(o instanceof GoString, "expected GoString, got " + o);
        return o.toString();
    }

    private static byte[] bytes(Object o) {
        assertTrue(o instanceof GoSlice, "expected GoSlice, got " + o);
        GoSlice gs = (GoSlice) o;
        byte[] arr = (byte[]) gs.array();
        byte[] out = new byte[gs.len()];
        System.arraycopy(arr, gs.offset(), out, 0, out.length);
        return out;
    }

    private static String hex(byte[] b) {
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte v : b) {
            sb.append(Character.forDigit((v >>> 4) & 0xF, 16));
            sb.append(Character.forDigit(v & 0xF, 16));
        }
        return sb.toString();
    }

    private static Object rsaCert() {
        Object c = GoX509.parse(der(RSA_CERT_B64));
        assertTrue(c != null, "rsa cert parses");
        return c;
    }

    public void testParseValidRsaCertFields() {
        Object c = rsaCert();
        assertTrue(str(GoX509.subject(c)).contains("example.com"),
                "subject: " + str(GoX509.subject(c)));
        assertTrue(str(GoX509.issuer(c)).contains("example.com"),
                "issuer: " + str(GoX509.issuer(c)));
        assertEq(3, (Long) GoX509.version(c), "version 3");
        // sha256WithRSAEncryption.
        assertEq("1.2.840.113549.1.1.11", str(GoX509.sigAlgOid(c)), "sig alg OID");
        assertTrue(bytes(GoX509.serialNumber(c)).length > 0, "serial non-empty");
        assertTrue(bytes(GoX509.signature(c)).length == 256, "RSA-2048 signature");
        assertTrue(bytes(GoX509.tbsCertificate(c)).length > 0, "TBS non-empty");
        assertTrue(bytes(GoX509.publicKeyEncoded(c)).length > 0, "SPKI non-empty");
        assertTrue(bytes(GoX509.rawSubject(c)).length > 0, "raw subject");
        assertTrue(bytes(GoX509.rawIssuer(c)).length > 0, "raw issuer");
        long notBefore = (Long) GoX509.notBefore(c);
        long notAfter = (Long) GoX509.notAfter(c);
        long now = System.currentTimeMillis();
        assertTrue(notBefore <= now && now <= notAfter,
                "validity window covers now: " + notBefore + ".." + notAfter);
        assertEq("DNS:example.com", str(GoX509.subjectAltNames(c)), "SAN list");
        assertEq("RSA", str(GoX509.publicKeyAlgOid(c)), "key alg");
        assertEq(65537, (Long) GoX509.rsaExponent(c), "RSA exponent");
        assertTrue(bytes(GoX509.rsaModulus(c)).length == 256, "RSA modulus 256 bytes");
        assertTrue(GoX509.ecX(c) == null, "ecX null for RSA cert");
        assertEq(-1, (Long) GoX509.ecFieldSize(c), "ecFieldSize -1 for RSA cert");
        assertTrue(GoX509.ed25519Raw(c) == null, "ed25519Raw null for RSA cert");
    }

    public void testParseGarbageReturnsNull() {
        assertTrue(GoX509.parse(der(java.util.Base64.getEncoder()
                .encodeToString("this is not a certificate".getBytes(
                        java.nio.charset.StandardCharsets.UTF_8)))) == null,
                "garbage -> null");
        assertTrue(GoX509.parse(sliceEmpty()) == null, "empty -> null");
        byte[] full = java.util.Base64.getDecoder().decode(RSA_CERT_B64);
        byte[] trunc = new byte[20];
        System.arraycopy(full, 0, trunc, 0, 20);
        assertTrue(GoX509.parse(new GoSlice(trunc, 0, 20, 20)) == null,
                "truncated DER -> null");
    }

    private static GoSlice sliceEmpty() {
        return new GoSlice(new byte[0], 0, 0, 0);
    }

    public void testSelfSignedSubjectEqualsIssuer() {
        Object c = rsaCert();
        assertEq(str(GoX509.subject(c)), str(GoX509.issuer(c)),
                "self-signed: subject == issuer");
        assertTrue(java.util.Arrays.equals(bytes(GoX509.rawSubject(c)),
                bytes(GoX509.rawIssuer(c))), "self-signed: raw subject == raw issuer");
    }

    public void testExpiredCertDetectedByValidityWindow() {
        Object c = GoX509.parse(der(EXPIRED_CERT_B64));
        assertTrue(c != null, "expired cert still parses");
        assertTrue(str(GoX509.subject(c)).contains("expired.example"),
                "subject: " + str(GoX509.subject(c)));
        long notAfter = (Long) GoX509.notAfter(c);
        assertTrue(notAfter < System.currentTimeMillis(),
                "notAfter in the past: " + notAfter);
        // The gc-style validity check the Go side must apply: it fails.
        long now = System.currentTimeMillis();
        boolean validNow = (Long) GoX509.notBefore(c) <= now && now <= notAfter;
        assertTrue(!validNow, "expired cert fails the validity-window check");
    }

    public void testWrongHostSanDoesNotMatch() {
        Object c = GoX509.parse(der(OTHER_CERT_B64));
        assertTrue(c != null, "wrong-host cert parses");
        assertEq("DNS:other.example", str(GoX509.subjectAltNames(c)), "SAN list");
        // A hostname check for example.com against this SAN list must fail.
        assertTrue(!str(GoX509.subjectAltNames(c)).contains("DNS:example.com"),
                "SAN list must not contain the requested host");
        assertTrue(str(GoX509.subject(c)).contains("other.example"), "subject CN");
    }

    public void testCaVsLeafBasicConstraints() {
        Object ca = GoX509.parse(der(CA_CERT_B64));
        assertTrue(ca != null, "CA cert parses");
        assertTrue((Long) GoX509.basicConstraints(ca) >= 0,
                "CA basicConstraints >= 0, was " + GoX509.basicConstraints(ca));
        assertEq(-1, (Long) GoX509.basicConstraints(rsaCert()),
                "leaf basicConstraints == -1 (not a CA)");
    }

    public void testKeyUsageAbsentReturnsMinusOne() {
        assertEq(-1, (Long) GoX509.keyUsage(rsaCert()),
                "no keyUsage extension -> -1");
    }

    public void testExtKeyUsageEmptyWhenAbsent() {
        assertEq("", str(GoX509.extKeyUsage(rsaCert())), "no EKU -> empty");
    }

    public void testSubjectAndAuthorityKeyId() {
        Object ski = GoX509.parse(der(SKI_CERT_B64));
        assertTrue(ski != null, "SKI cert parses");
        // openssl SKI hash for this cert (from its -text dump).
        assertEq("fd7ff364a21b09871af76f52a620c6f606c29a5b",
                hex(bytes(GoX509.subjectKeyId(ski))), "SKI bytes");
        assertEq("fd7ff364a21b09871af76f52a620c6f606c29a5b",
                hex(bytes(GoX509.authorityKeyId(ski))), "AKI bytes (self-signed)");
    }

    public void testNoExtensionsCertReportsAbsents() {
        // Every other fixture carries SKI/AKI (openssl and keytool add
        // them by default), so the absent-extension cases need this
        // dedicated no-extensions fixture.
        Object c = GoX509.parse(der(NOSKI_CERT_B64));
        assertTrue(c != null, "no-extensions cert parses");
        assertTrue(GoX509.subjectKeyId(c) == null, "no SKI extension -> null");
        assertTrue(GoX509.authorityKeyId(c) == null, "no AKI extension -> null");
        assertEq("", str(GoX509.subjectAltNames(c)), "no SAN -> empty");
        assertEq(-1, (Long) GoX509.keyUsage(c), "no keyUsage -> -1");
        assertEq("", str(GoX509.extKeyUsage(c)), "no EKU -> empty");
    }

    public void testEcCertFields() {
        Object c = GoX509.parse(der(EC_CERT_B64));
        assertTrue(c != null, "EC cert parses");
        assertEq("EC", str(GoX509.publicKeyAlgOid(c)), "key alg");
        assertEq(256, (Long) GoX509.ecFieldSize(c), "P-256 field size");
        byte[] x = bytes(GoX509.ecX(c));
        byte[] y = bytes(GoX509.ecY(c));
        assertTrue(x.length >= 31 && x.length <= 32, "ecX length: " + x.length);
        assertTrue(y.length >= 31 && y.length <= 32, "ecY length: " + y.length);
        assertTrue(GoX509.rsaModulus(c) == null, "rsaModulus null for EC cert");
        assertEq(-1, (Long) GoX509.rsaExponent(c), "rsaExponent -1 for EC cert");
        assertEq("DNS:ec.example", str(GoX509.subjectAltNames(c)), "EC SAN");
    }

    public void testEd25519CertFields() {
        Object c = GoX509.parse(der(ED_CERT_B64));
        assertTrue(c != null, "Ed25519 cert parses");
        // The JDK reports this key's algorithm as "EdDSA" on this
        // toolchain (not "Ed25519"); the Go side accepts both
        // spellings (pubKeyAlgFromName in synth_crypto_x509.go), so
        // the contract is membership in that pair, not one spelling.
        String alg = str(GoX509.publicKeyAlgOid(c));
        assertTrue("Ed25519".equals(alg) || "EdDSA".equals(alg),
                "key alg must be Ed25519 or EdDSA, was: " + alg);
        assertTrue(bytes(GoX509.ed25519Raw(c)).length == 32, "ed25519 raw key 32 bytes");
        String spki = str(GoX509.parseSpki(GoX509.publicKeyEncoded(c)));
        assertTrue(spki.startsWith("Ed25519:"), "parseSpki Ed25519: " + spki);
        assertEq(8 + 64, spki.length(), "Ed25519 SPKI encoding length");
    }

    public void testParseSpkiRsa() {
        String spki = str(GoX509.parseSpki(GoX509.publicKeyEncoded(rsaCert())));
        assertTrue(spki.startsWith("RSA:"), "parseSpki RSA: " + spki);
        assertTrue(spki.endsWith(":65537"), "parseSpki exponent: " + spki);
        String[] parts = spki.split(":");
        assertEq(3, parts.length, "RSA SPKI parts");
        assertEq(512, parts[1].length(), "RSA modulus hex length");
        assertTrue(GoX509.parseSpki(sliceEmpty()) == null, "parseSpki empty -> null");
    }

    public void testParsePkcs1() {
        String pkcs1 = str(GoX509.parsePkcs1(der(RSA_PKCS1_B64)));
        assertTrue(pkcs1.endsWith(":65537"), "PKCS1 exponent: " + pkcs1);
        String[] parts = pkcs1.split(":");
        assertEq(2, parts.length, "PKCS1 parts");
        assertEq(512, parts[0].length(), "PKCS1 modulus hex length");
        assertTrue(GoX509.parsePkcs1(sliceEmpty()) == null, "parsePkcs1 empty -> null");
    }

    public void testParsePrivateKeyFormats() {
        String rsa = str(GoX509.parsePrivateKey(der(RSA_PKCS8_B64)));
        assertTrue(rsa.startsWith("PKCS8:RSA:"), "RSA PKCS8: " + rsa.substring(0,
                Math.min(rsa.length(), 12)));
        String[] rparts = rsa.split(":");
        assertEq(7, rparts.length, "RSA PKCS8 parts (fmt, alg, n, e, d, p, q)");
        assertEq("010001", rparts[3], "RSA PKCS8 exponent hex (65537)");
        String ec = str(GoX509.parsePrivateKey(der(EC_SEC1_B64)));
        assertTrue(ec.startsWith("SEC1:EC:secp256r1:"), "EC SEC1: " + ec);
        String[] eparts = ec.split(":");
        assertEq(6, eparts.length, "EC SEC1 parts (fmt, alg, curve, x, y, d)");
        assertEq(64, eparts[3].length(), "EC x hex length");
        assertEq(64, eparts[4].length(), "EC y hex length");
        assertEq(64, eparts[5].length(), "EC d hex length");
        String ed = str(GoX509.parsePrivateKey(der(ED_PKCS8_B64)));
        assertTrue(ed.startsWith("PKCS8:Ed25519:"), "Ed25519 PKCS8: " + ed);
        // "PKCS8:" (6) + "Ed25519:" (8) + 64 hex chars of the 32-byte seed.
        assertEq(6 + 8 + 64, ed.length(), "Ed25519 seed encoding length");
        assertEq("", str(GoX509.parsePrivateKey(sliceEmpty())),
                "garbage private key -> empty string");
    }
}
