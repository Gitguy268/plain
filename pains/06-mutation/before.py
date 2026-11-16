# The default list is made once and shared between calls.
def add_item(item, items=[]):
    items.append(item)
    return items

print(add_item("a"))
print(add_item("b"))  # ['a', 'b'], not ['b']
