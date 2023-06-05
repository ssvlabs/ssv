import type { AppProps } from "next/app";
import "prism-themes/themes/prism-vs.css";
import "../globals.css"; // import global styles here

function MyApp({ Component, pageProps }: AppProps) {
  return <Component {...pageProps} />;
}
export default MyApp;
