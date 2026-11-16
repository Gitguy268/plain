// "pending" was added later, and this switch quietly returns undefined for it.
function label(status) {
  switch (status) {
    case "active":
      return "Active";
    case "banned":
      return "Banned";
  }
}
console.log(label("active"));
console.log(label("pending"));
