// The billion-dollar mistake: find() can return undefined, and nothing warns you.
const users = [{ id: 1, name: "Ann" }, { id: 2, name: "Bo" }];
const user = users.find((u) => u.id === 3);
console.log(user.name.toUpperCase());
