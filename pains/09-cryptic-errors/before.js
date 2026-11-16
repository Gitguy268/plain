// A typo becomes a runtime crash with a message about something else.
const scores = [3, 1, 2];
const total = scores.lenght + 1; // undefined + 1
console.log(total);              // NaN
console.log(scores.sortt());     // TypeError: scores.sortt is not a function
