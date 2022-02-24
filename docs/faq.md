# FAQ

**Q: I created a self-signed certificate but I cannot get the web app to work on my Apple device**

A: This is an Apple restriction where Websocket connections to a self-signed certificate are not allowed. Use the native app or have a real certificate.

**Q: I tried the autocert function but I get strange error messages**

A: Due to the ACME protocol used by Let's Encrypt, ports 80 and 443 must be open to the world for autocert to work.

**Q: I did not find an answer to my question in this FAQ**

A: No problem, just drop us a line at **[rdio-scanner@saubeo.solutions](mailto:rdio-scanner@saubeo.solutions)** and we'll make sure to add the relevant information in this document in the next release. In the meantime, You can ask your questions on the [Rdio Scanner Discussions](https://github.com/chuot/rdio-scanner/discussions) at **[https://github.com/chuot/rdio-scanner/discussions](https://github.com/chuot/rdio-scanner/discussions)**.

**Q: How do I update from version 5 to version 6**

A: First, don't use the update.js script, it won't work since the server portion of version 6 as been completely rewritten in GO. Simply unzip the archive that contains the Rdio Scanner executable and its PDF document to a new folder, then copy the database.sqlite from version 5 to the new folder that contains version 6 and make sure to rename it to _rdio-scanner.db_.

**Q: The web app keeps displaying a dialog telling me that a new version is available**

A: Force a refresh of the web application from the browser (usually with ctrl-shift-r) to resolve the issue. Alternatively, you can click on the icon just to the left of the URL address and select website settings, then clear all website data.

\pagebreak{}