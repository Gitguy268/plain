// One small mistake (sorting a std::list) gives pages of template errors.
#include <algorithm>
#include <list>

int main() {
  std::list<int> numbers = {3, 1, 2};
  std::sort(numbers.begin(), numbers.end());
}
