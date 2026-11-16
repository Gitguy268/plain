// Past 2^53, JavaScript numbers silently stop counting.
const big = 9007199254740992;
console.log(big + 1 === big); // true
console.log(big + 1);         // 9007199254740992
