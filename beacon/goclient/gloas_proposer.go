@@
-	// The Gloas publish endpoints do not return a body, but the original
-	// implementation set the Accept header to `application/octet-stream`,
-	// which causes Prysm to respond with 406 Not Acceptable.  The spec
-	// allows any Accept value because the response is empty, so we
-	// simply omit the header or use a wildcard.
-	//
-	// Previously:
-	//   req.Header.Set("Accept", "application/octet-stream")
-	//
-	// Updated to use a wildcard or no Accept header.
-	if headers == nil {
-		headers = make(map[string]string)
-	}
-	headers["Accept"] = "application/octet-stream"
+	// The Gloas publish endpoints do not return a body.  Some beacon
+	// clients (e.g. Prysm) reject requests that specify
+	// `Accept: application/octet-stream` with a 406 Not Acceptable
+	// response.  To maintain compatibility with all clients we
+	// remove the explicit Accept header or use a wildcard.  The
+	// server will respond with an empty body regardless of the
+	// Accept value, so a wildcard is safe.
+	if headers == nil {
+		headers = make(map[string]string)
+	}
+	// Do not set Accept to a specific value; use a wildcard if
+	// the caller has not provided one.
+	if _, ok := headers["Accept"]; !ok {
+		headers["Accept"] = "*/*"
+	}
