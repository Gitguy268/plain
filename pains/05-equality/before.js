// == converts types, === compares references, and neither compares contents.
console.log([1, 2] == [1, 2]);       // false
console.log(0 == "");                // true
console.log("1" == 1);               // true
console.log({ a: 1 } === { a: 1 });  // false
