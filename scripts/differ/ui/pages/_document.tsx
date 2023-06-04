// pages/_document.tsx
import Document, { Html, Head, Main, NextScript } from "next/document";

class MyDocument extends Document {
  render() {
    return (
      <Html>
        <Head>
          <link rel="preconnect" href="https://fonts.googleapis.com" />
          <link
            rel="preconnect"
            href="https://fonts.gstatic.com"
            crossOrigin=""
          />
          <link
            href="https://fonts.googleapis.com/css2?family=Recursive:wght,MONO@300..500,1&display=swap"
            rel="stylesheet"
          />
        </Head>
        <body className="bg-gray-200">
          <Main />
          <NextScript />
        </body>
      </Html>
    );
  }
}

export default MyDocument;
