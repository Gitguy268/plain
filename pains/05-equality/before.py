# Comparing different types is silently False, so a typo'd comparison never matches.
user_input = "42"
if user_input == 42:
    print("match")
else:
    print("no match")
