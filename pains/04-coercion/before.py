# Python refuses, but only when the line actually runs, which may be in production.
def receipt(total):
    if total > 100:
        return "Total: " + total  # TypeError, but only for big orders
    return "Total: " + str(total)

print(receipt(5))
print(receipt(500))
