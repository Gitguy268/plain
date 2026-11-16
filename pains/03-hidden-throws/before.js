// Nothing about decode() says it can throw, so nobody catches it and the program dies.
function decode(text) {
  return decodeURIComponent(text);
}
console.log(decode("caf%C3%A9"));
console.log(decode("100%")); // URIError: URI malformed
console.log("never printed");
