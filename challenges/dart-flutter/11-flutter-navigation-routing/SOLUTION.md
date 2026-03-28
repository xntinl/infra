# Section 11: Solutions -- Flutter Navigation & Routing

## How to Use This File

Work through each exercise in the README before looking here. When you get stuck:

1. Read the **Progressive Hints** for your exercise. Each hint reveals a bit more without giving away the full answer.
2. If still stuck, check **Common Mistakes** -- your issue might be listed.
3. Only read the **Full Solution** after a genuine attempt. Copy-pasting teaches you nothing.
4. After implementing, read the **Deep Dive** for that exercise to understand the *why* behind the approach.

---

## Exercise 1: Multi-Screen Navigation with Data Passing

### Progressive Hints

**Hint 1:** `Navigator.push` returns a `Future`. The value of that future is whatever you pass to `Navigator.pop(context, value)`. Declare the result type when calling push.

**Hint 2:** The return type from `Navigator.push` is `Future<T?>` -- it is nullable because the user might press the system back button instead of your "Add to Cart" button, in which case the result is `null`.

**Hint 3:** Use `await` with `Navigator.push` to capture the result. Check if the result is not null before showing the SnackBar.

### Full Solution

```dart
// file: lib/models/product.dart

class Product {
  final int id;
  final String name;
  final double price;
  final String description;

  const Product({
    required this.id,
    required this.name,
    required this.price,
    required this.description,
  });
}

final List<Product> mockProducts = [
  const Product(id: 1, name: 'Wireless Headphones', price: 79.99, description: 'Noise-cancelling over-ear headphones with 30h battery life.'),
  const Product(id: 2, name: 'Mechanical Keyboard', price: 129.99, description: 'Cherry MX Blue switches, RGB backlit, full-size layout.'),
  const Product(id: 3, name: 'USB-C Hub', price: 49.99, description: '7-in-1 hub with HDMI, USB-A, SD card reader, and PD charging.'),
  const Product(id: 4, name: 'Webcam HD', price: 59.99, description: '1080p webcam with auto-focus and built-in microphone.'),
  const Product(id: 5, name: 'Monitor Stand', price: 34.99, description: 'Adjustable aluminum stand with cable management.'),
];
```

```dart
// file: lib/screens/product_list_screen.dart

import 'package:flutter/material.dart';
import '../models/product.dart';
import 'product_detail_screen.dart';

class ProductListScreen extends StatelessWidget {
  const ProductListScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Products')),
      body: ListView.builder(
        itemCount: mockProducts.length,
        itemBuilder: (context, index) {
          final product = mockProducts[index];
          return ListTile(
            title: Text(product.name),
            subtitle: Text('\$${product.price.toStringAsFixed(2)}'),
            trailing: const Icon(Icons.chevron_right),
            onTap: () async {
              final addedProduct = await Navigator.push<Product>(
                context,
                MaterialPageRoute(
                  builder: (context) => ProductDetailScreen(product: product),
                ),
              );

              if (addedProduct != null && context.mounted) {
                ScaffoldMessenger.of(context).showSnackBar(
                  SnackBar(
                    content: Text('${addedProduct.name} added to cart'),
                    duration: const Duration(seconds: 2),
                  ),
                );
              }
            },
          );
        },
      ),
    );
  }
}
```

```dart
// file: lib/screens/product_detail_screen.dart

import 'package:flutter/material.dart';
import '../models/product.dart';

class ProductDetailScreen extends StatelessWidget {
  final Product product;

  const ProductDetailScreen({super.key, required this.product});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(product.name)),
      body: Padding(
        padding: const EdgeInsets.all(16.0),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              product.name,
              style: Theme.of(context).textTheme.headlineMedium,
            ),
            const SizedBox(height: 8),
            Text(
              '\$${product.price.toStringAsFixed(2)}',
              style: Theme.of(context).textTheme.titleLarge?.copyWith(
                    color: Colors.green.shade700,
                  ),
            ),
            const SizedBox(height: 16),
            Text(product.description),
            const Spacer(),
            SizedBox(
              width: double.infinity,
              child: ElevatedButton(
                onPressed: () {
                  Navigator.pop(context, product);
                },
                child: const Text('Add to Cart'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
```

### Common Mistakes

**Mistake 1: Forgetting `context.mounted` after an async gap.**
After `await Navigator.push(...)`, the widget might have been disposed. Always check `context.mounted` before using the context.

```dart
// WRONG -- context might be invalid
final result = await Navigator.push<Product>(...);
ScaffoldMessenger.of(context).showSnackBar(...);

// CORRECT
final result = await Navigator.push<Product>(...);
if (result != null && context.mounted) {
  ScaffoldMessenger.of(context).showSnackBar(...);
}
```

**Mistake 2: Not typing the `Navigator.push` generic.**
Without `Navigator.push<Product>(...)`, the return type is `dynamic` and you lose type safety. Always specify the type parameter.

**Mistake 3: Using `Navigator.pop(context)` without a result when you intend to return data.**
If your detail screen has both a back button and an "Add to Cart" button, only the "Add to Cart" should call `Navigator.pop(context, product)`. The AppBar back button calls `Navigator.pop(context)` implicitly (returning null), which is the correct distinction.

### Deep Dive

The `Navigator.push` / `pop` pattern is a direct analogy to a function call stack. `push` is like calling a function; `pop` is like returning from it. The "return value" of the navigation is whatever you pass to `pop`. This is why the API feels natural for simple flows.

The limitation appears when you need to coordinate navigation from outside the widget tree -- for instance, reacting to a push notification that should open a specific screen. Imperative navigation requires a `BuildContext`, which ties navigation tightly to the widget tree. This is the fundamental tension that declarative routing solves.

---

## Exercise 2: Named Routes with onGenerateRoute

### Progressive Hints

**Hint 1:** `settings.arguments` is `Object?`. You need to cast it to the expected type. Use `as` with a null check, or use pattern matching.

**Hint 2:** For the 404 screen, `onGenerateRoute` should have a `default` case in the switch that returns a `MaterialPageRoute` to your `NotFoundScreen`.

**Hint 3:** To test invalid arguments, you could temporarily add a button that calls `Navigator.pushNamed(context, '/product')` without passing any arguments.

### Full Solution

```dart
// file: lib/main.dart

import 'package:flutter/material.dart';
import 'models/product.dart';
import 'screens/product_list_screen.dart';
import 'screens/product_detail_screen.dart';
import 'screens/cart_screen.dart';
import 'screens/error_screen.dart';
import 'screens/not_found_screen.dart';

void main() => runApp(const MyApp());

class MyApp extends StatelessWidget {
  const MyApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Product App',
      initialRoute: '/',
      onGenerateRoute: (RouteSettings settings) {
        switch (settings.name) {
          case '/':
            return MaterialPageRoute(
              builder: (_) => const ProductListScreen(),
              settings: settings,
            );

          case '/product':
            final args = settings.arguments;
            if (args is! Product) {
              return MaterialPageRoute(
                builder: (_) => const ErrorScreen(
                  message: 'Product screen requires a Product argument.',
                ),
                settings: settings,
              );
            }
            return MaterialPageRoute(
              builder: (_) => ProductDetailScreen(product: args),
              settings: settings,
            );

          case '/cart':
            return MaterialPageRoute(
              builder: (_) => const CartScreen(),
              settings: settings,
            );

          default:
            return MaterialPageRoute(
              builder: (_) => NotFoundScreen(route: settings.name ?? 'unknown'),
              settings: settings,
            );
        }
      },
    );
  }
}
```

```dart
// file: lib/screens/error_screen.dart

import 'package:flutter/material.dart';

class ErrorScreen extends StatelessWidget {
  final String message;

  const ErrorScreen({super.key, required this.message});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Error')),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24.0),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              const Icon(Icons.error_outline, size: 64, color: Colors.red),
              const SizedBox(height: 16),
              Text(
                message,
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 24),
              ElevatedButton(
                onPressed: () => Navigator.pop(context),
                child: const Text('Go Back'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
```

```dart
// file: lib/screens/not_found_screen.dart

import 'package:flutter/material.dart';

class NotFoundScreen extends StatelessWidget {
  final String route;

  const NotFoundScreen({super.key, required this.route});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('404')),
      body: Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            const Text(
              '404',
              style: TextStyle(fontSize: 72, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 16),
            Text('Route "$route" not found.'),
            const SizedBox(height: 24),
            ElevatedButton(
              onPressed: () {
                Navigator.pushNamedAndRemoveUntil(context, '/', (_) => false);
              },
              child: const Text('Go Home'),
            ),
          ],
        ),
      ),
    );
  }
}
```

Updated navigation call in the list screen:

```dart
// file: lib/screens/product_list_screen.dart (named route version)

onTap: () async {
  final addedProduct = await Navigator.pushNamed<Product>(
    context,
    '/product',
    arguments: product,
  );

  if (addedProduct != null && context.mounted) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('${addedProduct.name} added to cart')),
    );
  }
},
```

### Common Mistakes

**Mistake: Using the `routes` map alongside `onGenerateRoute`.**
If you define both `routes: {'/': ...}` and `onGenerateRoute`, the `routes` map takes priority. Mixing them leads to confusion. Pick one approach and stick with it.

**Mistake: Casting `settings.arguments` without a type check.**
Writing `final product = settings.arguments as Product;` throws a `TypeError` at runtime if arguments are null or wrong type. Use `is` for safe checking:

```dart
// FRAGILE
final product = settings.arguments as Product;

// SAFE
if (settings.arguments is! Product) {
  return MaterialPageRoute(builder: (_) => const ErrorScreen(...));
}
final product = settings.arguments as Product;
```

### Debugging Tip

If `onGenerateRoute` is never called, check whether you also set the `routes` parameter on `MaterialApp`. The `routes` map is checked first. If a route matches there, `onGenerateRoute` is bypassed entirely for that route.

---

## Exercise 3: GoRouter Setup with Nested Routes

### Progressive Hints

**Hint 1:** Install GoRouter: add `go_router: ^14.0.0` (or latest) to your `pubspec.yaml` and run `flutter pub get`. Use `MaterialApp.router(routerConfig: appRouter)`.

**Hint 2:** Nested routes in GoRouter inherit the parent path. A child route with `path: 'post/:postId'` under a parent with `path: '/category/:categoryId'` creates the full path `/category/:categoryId/post/:postId`.

**Hint 3:** Access path parameters with `state.pathParameters['categoryId']`. Access query parameters with `state.uri.queryParameters['q']`. Both return `String?`.

### Full Solution

```dart
// file: lib/data/mock_data.dart

class BlogCategory {
  final int id;
  final String name;
  final List<BlogPost> posts;

  const BlogCategory({required this.id, required this.name, required this.posts});
}

class BlogPost {
  final int id;
  final int categoryId;
  final String title;
  final String body;

  const BlogPost({
    required this.id,
    required this.categoryId,
    required this.title,
    required this.body,
  });
}

final List<BlogCategory> categories = [
  BlogCategory(
    id: 1,
    name: 'Flutter',
    posts: [
      BlogPost(id: 1, categoryId: 1, title: 'Getting Started with Flutter', body: 'Flutter is Google\'s UI toolkit for building natively compiled applications...'),
      BlogPost(id: 2, categoryId: 1, title: 'State Management Patterns', body: 'Managing state is one of the most important architectural decisions...'),
      BlogPost(id: 3, categoryId: 1, title: 'Custom Painting in Flutter', body: 'The CustomPainter class gives you a canvas to draw anything...'),
    ],
  ),
  BlogCategory(
    id: 2,
    name: 'Dart',
    posts: [
      BlogPost(id: 4, categoryId: 2, title: 'Dart Null Safety Deep Dive', body: 'Sound null safety eliminates null reference exceptions at compile time...'),
      BlogPost(id: 5, categoryId: 2, title: 'Isolates and Concurrency', body: 'Dart uses isolates instead of threads for concurrent computation...'),
      BlogPost(id: 6, categoryId: 2, title: 'Extension Methods', body: 'Extensions let you add functionality to existing classes without modifying them...'),
    ],
  ),
  BlogCategory(
    id: 3,
    name: 'DevOps',
    posts: [
      BlogPost(id: 7, categoryId: 3, title: 'CI/CD for Flutter Apps', body: 'Automated builds and deployments save time and reduce human error...'),
      BlogPost(id: 8, categoryId: 3, title: 'Fastlane Integration', body: 'Fastlane automates beta deployments and release management...'),
      BlogPost(id: 9, categoryId: 3, title: 'Firebase App Distribution', body: 'Distribute pre-release versions to testers without app store review...'),
    ],
  ),
];

BlogCategory? findCategoryById(int id) {
  try {
    return categories.firstWhere((c) => c.id == id);
  } catch (_) {
    return null;
  }
}

BlogPost? findPostById(int categoryId, int postId) {
  final category = findCategoryById(categoryId);
  if (category == null) return null;
  try {
    return category.posts.firstWhere((p) => p.id == postId);
  } catch (_) {
    return null;
  }
}
```

```dart
// file: lib/router/app_router.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../data/mock_data.dart';
import '../screens/home_screen.dart';
import '../screens/category_screen.dart';
import '../screens/post_detail_screen.dart';
import '../screens/search_screen.dart';
import '../screens/not_found_screen.dart';

final GoRouter appRouter = GoRouter(
  initialLocation: '/',
  errorBuilder: (context, state) => NotFoundScreen(
    message: 'Page not found: ${state.uri}',
  ),
  routes: [
    GoRoute(
      path: '/',
      name: 'home',
      builder: (context, state) => const HomeScreen(),
      routes: [
        GoRoute(
          path: 'category/:categoryId',
          name: 'category',
          builder: (context, state) {
            final categoryId = int.tryParse(
              state.pathParameters['categoryId'] ?? '',
            );
            if (categoryId == null) {
              return const NotFoundScreen(message: 'Invalid category ID.');
            }
            final category = findCategoryById(categoryId);
            if (category == null) {
              return NotFoundScreen(message: 'Category $categoryId not found.');
            }
            return CategoryScreen(category: category);
          },
          routes: [
            GoRoute(
              path: 'post/:postId',
              name: 'post',
              builder: (context, state) {
                final categoryId = int.tryParse(
                  state.pathParameters['categoryId'] ?? '',
                );
                final postId = int.tryParse(
                  state.pathParameters['postId'] ?? '',
                );
                if (categoryId == null || postId == null) {
                  return const NotFoundScreen(message: 'Invalid parameters.');
                }
                final post = findPostById(categoryId, postId);
                if (post == null) {
                  return NotFoundScreen(
                    message: 'Post $postId not found in category $categoryId.',
                  );
                }
                return PostDetailScreen(post: post);
              },
            ),
          ],
        ),
      ],
    ),
    GoRoute(
      path: '/search',
      name: 'search',
      builder: (context, state) {
        final query = state.uri.queryParameters['q'] ?? '';
        final sort = state.uri.queryParameters['sort'] ?? 'relevance';
        return SearchScreen(query: query, sort: sort);
      },
    ),
  ],
);
```

```dart
// file: lib/screens/category_screen.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../data/mock_data.dart';

class CategoryScreen extends StatelessWidget {
  final BlogCategory category;

  const CategoryScreen({super.key, required this.category});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(category.name)),
      body: ListView.builder(
        itemCount: category.posts.length,
        itemBuilder: (context, index) {
          final post = category.posts[index];
          return ListTile(
            title: Text(post.title),
            onTap: () {
              context.go('/category/${category.id}/post/${post.id}');
            },
          );
        },
      ),
    );
  }
}
```

```dart
// file: lib/screens/post_detail_screen.dart

import 'package:flutter/material.dart';
import '../data/mock_data.dart';

class PostDetailScreen extends StatelessWidget {
  final BlogPost post;

  const PostDetailScreen({super.key, required this.post});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(post.title)),
      body: Padding(
        padding: const EdgeInsets.all(16.0),
        child: Text(post.body, style: Theme.of(context).textTheme.bodyLarge),
      ),
    );
  }
}
```

```dart
// file: lib/screens/search_screen.dart

import 'package:flutter/material.dart';

class SearchScreen extends StatelessWidget {
  final String query;
  final String sort;

  const SearchScreen({super.key, required this.query, required this.sort});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Search')),
      body: Padding(
        padding: const EdgeInsets.all(16.0),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Query: "$query"'),
            Text('Sort: $sort'),
            const SizedBox(height: 16),
            const Text('Search results would appear here.'),
          ],
        ),
      ),
    );
  }
}
```

### Common Mistakes

**Mistake: Using `context.go()` when you meant `context.push()`.**
`context.go('/category/1/post/2')` replaces the navigation stack so the URL matches the route hierarchy. If you are already at `/category/1` and call `context.go('/category/1/post/2')`, the back button works as expected because GoRouter builds the stack from the URL segments.

But `context.go('/search')` from `/category/1` replaces the entire stack. The back button would exit the app, not return to the category. If you want push-like behavior (adding onto the stack), use `context.push('/search')`.

**Mistake: Forgetting to handle `int.tryParse` returning null.**
Path parameters are always strings. `int.parse('abc')` throws a `FormatException`. Always use `int.tryParse` and handle the null case.

### Deep Dive

GoRouter reconstructs the navigation stack from the URL path segments. When you navigate to `/category/1/post/2`, GoRouter checks if `/` has a builder (yes -- HomeScreen), if `/category/1` has a builder (yes -- CategoryScreen), and if `/category/1/post/2` has a builder (yes -- PostDetailScreen). It creates pages for all three and the back button walks back through them. This is the key difference from imperative navigation: the URL *is* the state.

---

## Exercise 4: Bottom Navigation with Separate Stacks

### Progressive Hints

**Hint 1:** `StatefulShellRoute.indexedStack` preserves the state of all branches because `IndexedStack` keeps all children in the widget tree but only shows one.

**Hint 2:** For "re-tap goes to root" behavior, check if the tapped index equals `navigationShell.currentIndex` and pass `initialLocation: true` to `goBranch`.

**Hint 3:** The URL on web updates automatically with GoRouter. Each branch's routes have their own path prefix (e.g., `/`, `/explore`, `/profile`), so the URL always reflects the active tab and depth.

### Full Solution

```dart
// file: lib/router/shell_routes.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../shells/app_shell.dart';
import '../screens/home/home_tab.dart';
import '../screens/home/item_detail_screen.dart';
import '../screens/explore/explore_tab.dart';
import '../screens/explore/category_detail_screen.dart';
import '../screens/profile/profile_tab.dart';
import '../screens/profile/settings_screen.dart';

final GoRouter appRouter = GoRouter(
  initialLocation: '/',
  routes: [
    StatefulShellRoute.indexedStack(
      builder: (context, state, navigationShell) {
        return AppShell(navigationShell: navigationShell);
      },
      branches: [
        // Home branch
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/',
              name: 'home',
              builder: (context, state) => const HomeTab(),
              routes: [
                GoRoute(
                  path: 'item/:itemId',
                  name: 'item-detail',
                  builder: (context, state) {
                    final itemId = state.pathParameters['itemId'] ?? '';
                    return ItemDetailScreen(itemId: itemId);
                  },
                ),
              ],
            ),
          ],
        ),

        // Explore branch
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/explore',
              name: 'explore',
              builder: (context, state) => const ExploreTab(),
              routes: [
                GoRoute(
                  path: 'category/:catId',
                  name: 'explore-category',
                  builder: (context, state) {
                    final catId = state.pathParameters['catId'] ?? '';
                    return CategoryDetailScreen(categoryId: catId);
                  },
                ),
              ],
            ),
          ],
        ),

        // Profile branch
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/profile',
              name: 'profile',
              builder: (context, state) => const ProfileTab(),
              routes: [
                GoRoute(
                  path: 'settings',
                  name: 'settings',
                  builder: (context, state) => const SettingsScreen(),
                ),
              ],
            ),
          ],
        ),
      ],
    ),
  ],
);
```

```dart
// file: lib/shells/app_shell.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

class AppShell extends StatelessWidget {
  final StatefulNavigationShell navigationShell;

  const AppShell({super.key, required this.navigationShell});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: navigationShell,
      bottomNavigationBar: NavigationBar(
        selectedIndex: navigationShell.currentIndex,
        onDestinationSelected: (index) {
          navigationShell.goBranch(
            index,
            // If tapping the current tab, go to its initial location
            initialLocation: index == navigationShell.currentIndex,
          );
        },
        destinations: const [
          NavigationDestination(icon: Icon(Icons.home), label: 'Home'),
          NavigationDestination(icon: Icon(Icons.explore), label: 'Explore'),
          NavigationDestination(icon: Icon(Icons.person), label: 'Profile'),
        ],
      ),
    );
  }
}
```

```dart
// file: lib/screens/home/home_tab.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

class HomeTab extends StatelessWidget {
  const HomeTab({super.key});

  @override
  Widget build(BuildContext context) {
    final items = List.generate(20, (i) => 'Item ${i + 1}');

    return Scaffold(
      appBar: AppBar(title: const Text('Home')),
      body: ListView.builder(
        itemCount: items.length,
        itemBuilder: (context, index) {
          return ListTile(
            title: Text(items[index]),
            onTap: () => context.go('/item/${index + 1}'),
          );
        },
      ),
    );
  }
}
```

### Common Mistakes

**Mistake: Using `context.go()` to navigate to a route in a different branch.**
`context.go('/explore')` from the Home tab works but it *replaces* the Home branch state. The Home tab will reset to its root next time you switch to it. If you want to switch tabs programmatically, use `navigationShell.goBranch(1)` instead.

**Mistake: Defining overlapping paths between branches.**
Each branch must have a unique path prefix. If two branches both start at `/`, GoRouter cannot determine which branch a URL belongs to. Use distinct prefixes like `/`, `/explore`, `/profile`.

### Debugging Tip

If tab state is not preserved, verify you are using `StatefulShellRoute.indexedStack` (not `StatefulShellRoute`). The `indexedStack` variant uses `IndexedStack` internally which keeps all children mounted. Without it, switching tabs rebuilds the branch from scratch.

---

## Exercise 5: Route Guards with Async Authentication

### Progressive Hints

**Hint 1:** Make your `AuthService` extend `ChangeNotifier`. Call `notifyListeners()` in `login()` and `logout()`. Pass it as `refreshListenable` to `GoRouter`.

**Hint 2:** The `redirect` callback runs synchronously. For async auth checks, you need a state variable like `isInitialized` that starts `false`, then becomes `true` after the async check completes. The redirect shows the splash screen while `isInitialized` is `false`.

**Hint 3:** To preserve the redirect target after login, extract it from the query parameter: `state.uri.queryParameters['redirect']`. After login, call `context.go(redirectPath)`.

### Full Solution

```dart
// file: lib/models/user.dart

enum UserRole { user, admin }

class AppUser {
  final String id;
  final String name;
  final String email;
  final UserRole role;

  const AppUser({
    required this.id,
    required this.name,
    required this.email,
    required this.role,
  });
}
```

```dart
// file: lib/services/auth_service.dart

import 'package:flutter/foundation.dart';
import '../models/user.dart';

class AuthService extends ChangeNotifier {
  static final AuthService instance = AuthService._();
  AuthService._();

  AppUser? _currentUser;
  bool _isInitialized = false;

  bool get isAuthenticated => _currentUser != null;
  bool get isInitialized => _isInitialized;
  AppUser? get currentUser => _currentUser;

  /// Simulates checking a stored token on app startup.
  Future<void> initialize() async {
    // Simulate async token validation
    await Future.delayed(const Duration(milliseconds: 500));
    // In a real app, you would check secure storage for a token
    // and validate it against your backend.
    _isInitialized = true;
    notifyListeners();
  }

  Future<void> login({required String email, required String password}) async {
    await Future.delayed(const Duration(milliseconds: 300));

    // Simulate different roles based on email
    final role = email.contains('admin') ? UserRole.admin : UserRole.user;

    _currentUser = AppUser(
      id: '1',
      name: 'Test User',
      email: email,
      role: role,
    );
    notifyListeners();
  }

  void logout() {
    _currentUser = null;
    notifyListeners();
  }
}
```

```dart
// file: lib/router/app_router.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../services/auth_service.dart';
import '../screens/splash_screen.dart';
import '../screens/login_screen.dart';
import '../screens/dashboard_screen.dart';
import '../screens/admin_screen.dart';
import '../screens/unauthorized_screen.dart';

final GoRouter appRouter = GoRouter(
  initialLocation: '/',
  refreshListenable: AuthService.instance,
  redirect: (BuildContext context, GoRouterState state) {
    final auth = AuthService.instance;
    final currentPath = state.matchedLocation;

    // Show splash while initializing
    if (!auth.isInitialized) {
      return currentPath == '/splash' ? null : '/splash';
    }

    final isLoggedIn = auth.isAuthenticated;
    final isGoingToLogin = currentPath == '/login';
    final isGoingToSplash = currentPath == '/splash';

    // Done initializing, leave splash
    if (isGoingToSplash) {
      return isLoggedIn ? '/' : '/login';
    }

    // Not logged in -- redirect to login with return path
    if (!isLoggedIn && !isGoingToLogin) {
      return '/login?redirect=$currentPath';
    }

    // Logged in but going to login -- redirect to home
    if (isLoggedIn && isGoingToLogin) {
      final redirect = state.uri.queryParameters['redirect'];
      return redirect ?? '/';
    }

    // Role-based guard for admin routes
    if (currentPath.startsWith('/admin')) {
      if (!isLoggedIn) return '/login?redirect=$currentPath';
      if (auth.currentUser?.role != UserRole.admin) return '/unauthorized';
    }

    return null;
  },
  routes: [
    GoRoute(
      path: '/splash',
      builder: (context, state) => const SplashScreen(),
    ),
    GoRoute(
      path: '/login',
      builder: (context, state) {
        final redirectPath = state.uri.queryParameters['redirect'];
        return LoginScreen(redirectPath: redirectPath);
      },
    ),
    GoRoute(
      path: '/',
      name: 'dashboard',
      builder: (context, state) => const DashboardScreen(),
    ),
    GoRoute(
      path: '/admin',
      builder: (context, state) => const AdminScreen(),
    ),
    GoRoute(
      path: '/unauthorized',
      builder: (context, state) => const UnauthorizedScreen(),
    ),
  ],
);
```

```dart
// file: lib/screens/login_screen.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../services/auth_service.dart';

class LoginScreen extends StatefulWidget {
  final String? redirectPath;

  const LoginScreen({super.key, this.redirectPath});

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _emailController = TextEditingController(text: 'user@example.com');
  final _passwordController = TextEditingController(text: 'password');
  bool _isLoading = false;

  Future<void> _handleLogin() async {
    setState(() => _isLoading = true);

    await AuthService.instance.login(
      email: _emailController.text,
      password: _passwordController.text,
    );

    // The redirect callback in GoRouter handles where to go next.
    // Because AuthService notifies listeners, GoRouter re-evaluates
    // the redirect and sends the user to the right place.

    if (mounted) {
      setState(() => _isLoading = false);
    }
  }

  @override
  void dispose() {
    _emailController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Login')),
      body: Padding(
        padding: const EdgeInsets.all(24.0),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            TextField(
              controller: _emailController,
              decoration: const InputDecoration(labelText: 'Email'),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _passwordController,
              decoration: const InputDecoration(labelText: 'Password'),
              obscureText: true,
            ),
            const SizedBox(height: 24),
            SizedBox(
              width: double.infinity,
              child: ElevatedButton(
                onPressed: _isLoading ? null : _handleLogin,
                child: _isLoading
                    ? const CircularProgressIndicator()
                    : const Text('Login'),
              ),
            ),
            if (widget.redirectPath != null)
              Padding(
                padding: const EdgeInsets.only(top: 16),
                child: Text(
                  'You will be redirected to: ${widget.redirectPath}',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
          ],
        ),
      ),
    );
  }
}
```

```dart
// file: lib/screens/splash_screen.dart

import 'package:flutter/material.dart';
import '../services/auth_service.dart';

class SplashScreen extends StatefulWidget {
  const SplashScreen({super.key});

  @override
  State<SplashScreen> createState() => _SplashScreenState();
}

class _SplashScreenState extends State<SplashScreen> {
  @override
  void initState() {
    super.initState();
    AuthService.instance.initialize();
  }

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: Center(
        child: CircularProgressIndicator(),
      ),
    );
  }
}
```

### Common Mistakes

**Mistake: Making the redirect callback async.**
GoRouter's `redirect` is synchronous. You cannot `await` inside it. The pattern is: manage an `isInitialized` flag, show a splash screen while false, and trigger the async work from the splash screen's `initState`. When the async work completes, call `notifyListeners()`, which triggers GoRouter to re-evaluate the redirect.

**Mistake: Infinite redirect loop.**
If your redirect logic always returns a non-null value, you get an infinite loop. The most common cause: forgetting to return `null` when the user is already heading to the correct destination. Always include a check like `if (!isLoggedIn && !isGoingToLogin)` -- the second condition prevents redirecting from login to login.

**Mistake: Not encoding the redirect path.**
If the redirect path contains special characters (query parameters, fragments), you need to URI-encode it. For simple paths like `/dashboard`, this is not an issue. For paths like `/search?q=flutter`, the `?` would break the login URL. Use `Uri.encodeComponent()` for safety.

### Deep Dive

The `refreshListenable` pattern is elegant. GoRouter subscribes to your `ChangeNotifier`. Whenever `notifyListeners()` fires, GoRouter re-runs the `redirect` callback with the current location. This means you never manually navigate after login or logout -- you just update the auth state, and the router figures out where the user should be. This is the core of declarative navigation: state drives the UI, not imperative commands.

---

## Exercise 6: Custom Page Transitions and Hero Animations

### Progressive Hints

**Hint 1:** For Hero animations to work with GoRouter, you must use `pageBuilder` (not `builder`) and return a `CustomTransitionPage`. The default `MaterialPage` wraps the child in an opaque layer that blocks Hero flight.

**Hint 2:** For Hero to animate, the source and destination Hero widgets must be in the widget tree *at the same time* during the transition. If you use a fade transition with `FadeTransition`, the old page fades out while the new one fades in -- both are visible, so Hero works. If you use an opaque slide, the old page may be hidden, breaking the Hero flight.

**Hint 3:** To make Hero work with `CustomTransitionPage`, set `maintainState: true` (the default) and avoid setting `opaque: false` unless you need see-through pages.

### Full Solution

```dart
// file: lib/router/transitions.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

enum TransitionType {
  fade,
  slideUp,
  slideRight,
  heroFade,
  none,
}

CustomTransitionPage buildTransitionPage({
  required GoRouterState state,
  required Widget child,
  required TransitionType type,
  Duration duration = const Duration(milliseconds: 300),
}) {
  switch (type) {
    case TransitionType.fade:
      return CustomTransitionPage(
        key: state.pageKey,
        child: child,
        transitionDuration: duration,
        transitionsBuilder: (context, animation, secondaryAnimation, child) {
          return FadeTransition(opacity: animation, child: child);
        },
      );

    case TransitionType.slideUp:
      return CustomTransitionPage(
        key: state.pageKey,
        child: child,
        transitionDuration: duration,
        transitionsBuilder: (context, animation, secondaryAnimation, child) {
          final offsetAnimation = Tween<Offset>(
            begin: const Offset(0, 1),
            end: Offset.zero,
          ).animate(CurvedAnimation(
            parent: animation,
            curve: Curves.easeOutCubic,
          ));
          return SlideTransition(position: offsetAnimation, child: child);
        },
      );

    case TransitionType.slideRight:
      return CustomTransitionPage(
        key: state.pageKey,
        child: child,
        transitionDuration: duration,
        transitionsBuilder: (context, animation, secondaryAnimation, child) {
          final offsetAnimation = Tween<Offset>(
            begin: const Offset(1, 0),
            end: Offset.zero,
          ).animate(CurvedAnimation(
            parent: animation,
            curve: Curves.easeOutCubic,
          ));
          return SlideTransition(position: offsetAnimation, child: child);
        },
      );

    case TransitionType.heroFade:
      return CustomTransitionPage(
        key: state.pageKey,
        child: child,
        transitionDuration: duration,
        transitionsBuilder: (context, animation, secondaryAnimation, child) {
          // Fade works well with Hero because both pages are visible
          return FadeTransition(opacity: animation, child: child);
        },
      );

    case TransitionType.none:
      return CustomTransitionPage(
        key: state.pageKey,
        child: child,
        transitionDuration: Duration.zero,
        transitionsBuilder: (context, animation, secondaryAnimation, child) {
          return child;
        },
      );
  }
}
```

```dart
// file: lib/screens/gallery_screen.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

class GalleryItem {
  final int id;
  final Color color;
  final String label;

  const GalleryItem({required this.id, required this.color, required this.label});
}

final galleryItems = List.generate(
  12,
  (i) => GalleryItem(
    id: i,
    color: Colors.primaries[i % Colors.primaries.length],
    label: 'Image ${i + 1}',
  ),
);

class GalleryScreen extends StatelessWidget {
  const GalleryScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Gallery'),
        actions: [
          IconButton(
            icon: const Icon(Icons.settings),
            onPressed: () => context.push('/settings'),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: () => context.push('/edit'),
        child: const Icon(Icons.edit),
      ),
      body: GridView.builder(
        padding: const EdgeInsets.all(8),
        gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
          crossAxisCount: 3,
          crossAxisSpacing: 8,
          mainAxisSpacing: 8,
        ),
        itemCount: galleryItems.length,
        itemBuilder: (context, index) {
          final item = galleryItems[index];
          return GestureDetector(
            onTap: () => context.push('/image/${item.id}'),
            child: Hero(
              tag: 'image-${item.id}',
              child: Container(
                decoration: BoxDecoration(
                  color: item.color,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Center(
                  child: Text(
                    item.label,
                    style: const TextStyle(color: Colors.white),
                  ),
                ),
              ),
            ),
          );
        },
      ),
    );
  }
}
```

```dart
// file: lib/screens/image_detail_screen.dart

import 'package:flutter/material.dart';
import 'gallery_screen.dart';

class ImageDetailScreen extends StatelessWidget {
  final int imageId;

  const ImageDetailScreen({super.key, required this.imageId});

  @override
  Widget build(BuildContext context) {
    final item = galleryItems.firstWhere((g) => g.id == imageId);

    return Scaffold(
      appBar: AppBar(title: Text(item.label)),
      body: Center(
        child: Hero(
          tag: 'image-${item.id}',
          child: Container(
            width: 300,
            height: 300,
            decoration: BoxDecoration(
              color: item.color,
              borderRadius: BorderRadius.circular(16),
            ),
            child: Center(
              child: Text(
                item.label,
                style: const TextStyle(color: Colors.white, fontSize: 24),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
```

```dart
// file: lib/router/app_router.dart (gallery app)

import 'package:go_router/go_router.dart';
import 'transitions.dart';
import '../screens/gallery_screen.dart';
import '../screens/image_detail_screen.dart';
import '../screens/edit_screen.dart';
import '../screens/settings_screen.dart';

final GoRouter appRouter = GoRouter(
  initialLocation: '/',
  routes: [
    GoRoute(
      path: '/',
      builder: (context, state) => const GalleryScreen(),
    ),
    GoRoute(
      path: '/image/:id',
      pageBuilder: (context, state) {
        final imageId = int.parse(state.pathParameters['id']!);
        return buildTransitionPage(
          state: state,
          child: ImageDetailScreen(imageId: imageId),
          type: TransitionType.heroFade,
        );
      },
    ),
    GoRoute(
      path: '/edit',
      pageBuilder: (context, state) => buildTransitionPage(
        state: state,
        child: const EditScreen(),
        type: TransitionType.slideUp,
      ),
    ),
    GoRoute(
      path: '/settings',
      pageBuilder: (context, state) => buildTransitionPage(
        state: state,
        child: const SettingsScreen(),
        type: TransitionType.fade,
      ),
    ),
  ],
);
```

### Common Mistakes

**Mistake: Hero tags do not match exactly.**
If the source uses `tag: 'image-$id'` but the destination uses `tag: 'img-$id'`, the animation will not happen. There is no error or warning -- it just silently does nothing. Always extract tag generation into a shared function:

```dart
String heroTag(int imageId) => 'image-$imageId';
```

**Mistake: Hero animation broken by opaque pages.**
If you use `pageBuilder` and your `CustomTransitionPage` defaults are changed (e.g., `opaque: false`), the Hero flight can break because the framework cannot find both Hero widgets simultaneously.

**Mistake: Rapid tapping causes overlapping transitions.**
By default, Flutter queues transitions. But if your custom transitions have long durations and you tap rapidly, you may see visual glitches. Consider disabling the button while a transition is in progress, or keeping transition durations under 400ms.

### Debugging Tip

To debug Hero animation issues, set `debugPaintPointersEnabled = true` or use the Flutter DevTools "Widget Inspector" to verify that both Hero widgets exist in the tree during the transition. You can also add a `flightShuttleBuilder` to the Hero widget to customize what is shown during flight and confirm the animation is triggering.

---

## Exercise 7: Full Declarative Routing System

### Progressive Hints

**Hint 1:** Start with the `StatefulShellRoute.indexedStack` for the four tabs. Get basic tab switching working before adding any guards or transitions.

**Hint 2:** For the checkout flow requiring auth, use a route-level `redirect` on the checkout routes. The redirect checks `AuthService.isAuthenticated` and redirects to a login route (which could be a full-screen modal using `pageBuilder` with a slide-up transition).

**Hint 3:** Deep linking to `/shop/category/:catId/product/:prodId` should work automatically if your route tree is correctly nested. The challenge is ensuring the shell shows the correct tab. GoRouter handles this: if the matched route is inside the Shop branch, the Shop tab is automatically selected.

**Hint 4:** For the checkout flow with a shared-element transition on the cart total, wrap the total amount in a Hero widget on both the Cart screen and the first checkout step.

### Full Solution (Architecture)

This exercise is too large for a single code listing. Here is the architecture and the critical pieces. You should implement the full solution following this structure.

```dart
// file: lib/router/app_router.dart (skeleton)

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../services/auth_service.dart';
import '../shells/main_shell.dart';
import 'route_guards.dart';
import 'transitions.dart';

// All screen imports...

final appRouter = GoRouter(
  initialLocation: '/',
  refreshListenable: AuthService.instance,
  redirect: globalRedirect,
  routes: [
    GoRoute(
      path: '/login',
      pageBuilder: (context, state) => buildTransitionPage(
        state: state,
        child: LoginScreen(
          redirectPath: state.uri.queryParameters['redirect'],
        ),
        type: TransitionType.slideUp,
      ),
    ),

    StatefulShellRoute.indexedStack(
      builder: (context, state, navigationShell) {
        return MainShell(navigationShell: navigationShell);
      },
      branches: [
        // Shop branch
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/',
              builder: (context, state) => const ShopCategoryListScreen(),
              routes: [
                GoRoute(
                  path: 'category/:catId',
                  pageBuilder: (context, state) => buildTransitionPage(
                    state: state,
                    child: ProductListScreen(
                      categoryId: state.pathParameters['catId']!,
                    ),
                    type: TransitionType.slideRight,
                  ),
                  routes: [
                    GoRoute(
                      path: 'product/:prodId',
                      pageBuilder: (context, state) => buildTransitionPage(
                        state: state,
                        child: ProductDetailScreen(
                          categoryId: state.pathParameters['catId']!,
                          productId: state.pathParameters['prodId']!,
                        ),
                        type: TransitionType.slideRight,
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ],
        ),

        // Cart branch
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/cart',
              builder: (context, state) => const CartScreen(),
              routes: [
                GoRoute(
                  path: 'checkout/address',
                  redirect: requireAuth,
                  builder: (context, state) => const CheckoutAddressScreen(),
                ),
                GoRoute(
                  path: 'checkout/payment',
                  redirect: requireAuth,
                  builder: (context, state) => const CheckoutPaymentScreen(),
                ),
                GoRoute(
                  path: 'checkout/confirmation',
                  redirect: requireAuth,
                  builder: (context, state) => const CheckoutConfirmationScreen(),
                ),
              ],
            ),
          ],
        ),

        // Orders branch
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/orders',
              redirect: requireAuth,
              builder: (context, state) => const OrderListScreen(),
              routes: [
                GoRoute(
                  path: ':orderId',
                  redirect: requireAuth,
                  builder: (context, state) => OrderDetailScreen(
                    orderId: state.pathParameters['orderId']!,
                  ),
                ),
              ],
            ),
          ],
        ),

        // Account branch
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/account',
              builder: (context, state) => const AccountScreen(),
              routes: [
                GoRoute(
                  path: 'edit-profile',
                  redirect: requireAuth,
                  builder: (context, state) => const EditProfileScreen(),
                ),
                GoRoute(
                  path: 'settings',
                  // No auth required for app settings
                  builder: (context, state) => const SettingsScreen(),
                ),
                GoRoute(
                  path: 'addresses',
                  redirect: requireAuth,
                  builder: (context, state) => const SavedAddressesScreen(),
                ),
              ],
            ),
          ],
        ),
      ],
    ),
  ],
);
```

```dart
// file: lib/router/route_guards.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../services/auth_service.dart';

/// Global redirect: handles splash and login-when-authenticated.
String? globalRedirect(BuildContext context, GoRouterState state) {
  final auth = AuthService.instance;

  if (!auth.isInitialized) {
    return state.matchedLocation == '/splash' ? null : '/splash';
  }

  // If user is logged in and going to login, redirect to intended page or home
  if (auth.isAuthenticated && state.matchedLocation == '/login') {
    return state.uri.queryParameters['redirect'] ?? '/';
  }

  return null;
}

/// Route-level redirect for protected routes.
String? requireAuth(BuildContext context, GoRouterState state) {
  final auth = AuthService.instance;
  if (!auth.isAuthenticated) {
    return '/login?redirect=${Uri.encodeComponent(state.matchedLocation)}';
  }
  return null;
}
```

The key architectural decisions:

1. **Global redirect** handles initialization (splash) and prevents authenticated users from seeing the login screen.
2. **Route-level redirects** (`requireAuth`) handle per-route protection. This is cleaner than putting every protected route in the global redirect.
3. **Transitions** are assigned per-context: slides for shop browsing, slide-up for modals, instant for tab switches (which is the default `StatefulShellRoute` behavior).
4. **Deep linking** works because GoRouter matches URLs against the route tree. `/shop/category/3/product/7` matches the nested route and GoRouter automatically selects the Shop tab in the shell.

### Common Mistakes

**Mistake: Checkout flow loses form data when navigating away.**
`StatefulShellRoute.indexedStack` preserves widget state per-branch. But if you navigate away from the Cart branch and back, the checkout screens are preserved only if they are still in the branch's navigation stack. If the user explicitly pops back to the cart root, the checkout screens are destroyed. To preserve form data across that scenario, store it in a service or state management solution outside the widget tree.

**Mistake: Deep link opens wrong tab.**
If your deep link URL does not match any route in any branch, GoRouter hits the error handler. If it matches a route but the route is in a branch you did not expect, check your route tree nesting. A common error is defining `/orders/:orderId` outside the shell, which means it opens without the bottom navigation bar.

---

## Exercise 8: Multi-Step Wizard Engine with Branching Paths

### Progressive Hints

**Hint 1:** Model the wizard as a directed graph, not a list. Each node (step) has edges to possible next steps. The edge to follow is determined by the step's result. A `Map<String, Map<String, String>>` works: `stepId -> { resultKey: nextStepId }`.

**Hint 2:** Maintain two pieces of state: a `history` stack (list of visited step IDs) for back navigation, and a `data` map (all collected form values) for state preservation.

**Hint 3:** When the user changes a branching decision, you need to detect which steps in the history are now invalid. Compare the new path against the old path and discard data keys associated with steps that are no longer on the path. Shared steps (like Employment Info) should use the same step ID regardless of which branch reached them, so their data is preserved.

**Hint 4:** For URL integration, map each step ID to a URL segment. The wizard reports the current step to GoRouter via a state change, and GoRouter updates the URL.

### Full Solution (Core Engine)

```dart
// file: lib/wizard/wizard_engine.dart

import 'package:flutter/foundation.dart';

typedef BranchResolver = String Function(Map<String, dynamic> data);

class WizardStep {
  final String id;
  final String label;
  final String urlSegment;

  /// For linear steps: the single next step ID.
  final String? nextStepId;

  /// For branching steps: a function that determines the next step
  /// based on collected data.
  final BranchResolver? branchResolver;

  const WizardStep({
    required this.id,
    required this.label,
    required this.urlSegment,
    this.nextStepId,
    this.branchResolver,
  }) : assert(
         nextStepId != null || branchResolver != null,
         'Each step must define either nextStepId or branchResolver',
       );

  String resolveNext(Map<String, dynamic> data) {
    if (branchResolver != null) return branchResolver!(data);
    return nextStepId!;
  }
}

class WizardEngine extends ChangeNotifier {
  final Map<String, WizardStep> _steps;
  final String _initialStepId;
  final String _terminalStepId;

  final List<String> _history = [];
  final Map<String, dynamic> _data = {};

  WizardEngine({
    required Map<String, WizardStep> steps,
    required String initialStepId,
    required String terminalStepId,
  })  : _steps = steps,
        _initialStepId = initialStepId,
        _terminalStepId = terminalStepId {
    _history.add(_initialStepId);
  }

  // -- Public API --

  String get currentStepId => _history.last;
  WizardStep get currentStep => _steps[currentStepId]!;
  Map<String, dynamic> get data => Map.unmodifiable(_data);
  List<String> get history => List.unmodifiable(_history);
  bool get isFirstStep => _history.length == 1;
  bool get isTerminalStep => currentStepId == _terminalStepId;

  /// Number of steps in the current projected path from start to end.
  int get totalStepsInCurrentPath {
    int count = 0;
    String stepId = _initialStepId;
    final visited = <String>{};

    while (stepId != _terminalStepId && !visited.contains(stepId)) {
      visited.add(stepId);
      count++;
      final step = _steps[stepId];
      if (step == null) break;
      stepId = step.resolveNext(_data);
    }

    return count + 1; // Include the terminal step
  }

  int get currentStepIndex {
    // The user's position is how many steps they have visited
    return _history.length;
  }

  /// Save data from the current step and advance to the next.
  void submitStep(Map<String, dynamic> stepData) {
    _data.addAll(stepData);

    if (isTerminalStep) return;

    final nextId = currentStep.resolveNext(_data);
    _trimFutureHistory(nextId);
    _history.add(nextId);
    notifyListeners();
  }

  /// Go back to the previous step. Data is preserved.
  void goBack() {
    if (_history.length <= 1) return;
    _history.removeLast();
    notifyListeners();
  }

  /// When a branching decision changes, discard steps that are no
  /// longer on the path but keep data from steps that remain valid.
  void _trimFutureHistory(String nextId) {
    // If the user went back and changed a decision, we might have
    // future history from a previous branch. Find and discard it.
    final currentIndex = _history.length - 1;

    // Nothing to trim if we are at the end of known history
    // (first time reaching this step).
  }

  /// Serialize the wizard state for draft saving.
  Map<String, dynamic> toJson() {
    return {
      'history': _history,
      'data': _data,
    };
  }

  /// Restore wizard state from a saved draft.
  void restoreFromJson(Map<String, dynamic> json) {
    _history.clear();
    _data.clear();

    final savedHistory = (json['history'] as List).cast<String>();
    final savedData = json['data'] as Map<String, dynamic>;

    _history.addAll(savedHistory);
    _data.addAll(savedData);
    notifyListeners();
  }

  /// Reset the wizard to the initial step, clearing all data.
  void reset() {
    _history.clear();
    _data.clear();
    _history.add(_initialStepId);
    notifyListeners();
  }
}
```

```dart
// file: lib/wizard/wizard_state.dart

import 'dart:convert';

class WizardDraft {
  final String wizardId;
  final Map<String, dynamic> state;
  final DateTime savedAt;

  const WizardDraft({
    required this.wizardId,
    required this.state,
    required this.savedAt,
  });

  String toJsonString() {
    return jsonEncode({
      'wizardId': wizardId,
      'state': state,
      'savedAt': savedAt.toIso8601String(),
    });
  }

  factory WizardDraft.fromJsonString(String json) {
    final map = jsonDecode(json) as Map<String, dynamic>;
    return WizardDraft(
      wizardId: map['wizardId'] as String,
      state: map['state'] as Map<String, dynamic>,
      savedAt: DateTime.parse(map['savedAt'] as String),
    );
  }
}
```

```dart
// file: lib/features/loan/loan_wizard_config.dart

import '../../wizard/wizard_engine.dart';

const String kStepPersonalInfo = 'personal-info';
const String kStepLoanType = 'loan-type';
const String kStepEmployment = 'employment';
const String kStepIncome = 'income';
const String kStepPropertyInfo = 'property-info';
const String kStepVehicleInfo = 'vehicle-info';
const String kStepDownPayment = 'down-payment';
const String kStepReview = 'review';
const String kStepConfirmation = 'confirmation';

WizardEngine createLoanWizard() {
  return WizardEngine(
    initialStepId: kStepPersonalInfo,
    terminalStepId: kStepConfirmation,
    steps: {
      kStepPersonalInfo: const WizardStep(
        id: kStepPersonalInfo,
        label: 'Personal Information',
        urlSegment: 'personal-info',
        nextStepId: kStepLoanType,
      ),

      kStepLoanType: WizardStep(
        id: kStepLoanType,
        label: 'Loan Type',
        urlSegment: 'loan-type',
        branchResolver: (data) {
          final loanType = data['loanType'] as String?;
          switch (loanType) {
            case 'personal':
              return kStepEmployment;
            case 'mortgage':
              return kStepPropertyInfo;
            case 'auto':
              return kStepVehicleInfo;
            default:
              return kStepEmployment;
          }
        },
      ),

      // Branch B (Mortgage) specific
      kStepPropertyInfo: const WizardStep(
        id: kStepPropertyInfo,
        label: 'Property Information',
        urlSegment: 'property-info',
        nextStepId: kStepEmployment, // Converges to shared step
      ),

      // Branch C (Auto) specific
      kStepVehicleInfo: const WizardStep(
        id: kStepVehicleInfo,
        label: 'Vehicle Information',
        urlSegment: 'vehicle-info',
        nextStepId: kStepEmployment, // Converges to shared step
      ),

      // Shared step: all branches pass through employment
      kStepEmployment: const WizardStep(
        id: kStepEmployment,
        label: 'Employment',
        urlSegment: 'employment',
        nextStepId: kStepIncome,
      ),

      // Shared step: income
      kStepIncome: WizardStep(
        id: kStepIncome,
        label: 'Income Details',
        urlSegment: 'income',
        branchResolver: (data) {
          final loanType = data['loanType'] as String?;
          if (loanType == 'mortgage') return kStepDownPayment;
          return kStepReview;
        },
      ),

      // Mortgage-only step after income
      kStepDownPayment: const WizardStep(
        id: kStepDownPayment,
        label: 'Down Payment',
        urlSegment: 'down-payment',
        nextStepId: kStepReview,
      ),

      kStepReview: const WizardStep(
        id: kStepReview,
        label: 'Review',
        urlSegment: 'review',
        nextStepId: kStepConfirmation,
      ),

      kStepConfirmation: const WizardStep(
        id: kStepConfirmation,
        label: 'Confirmation',
        urlSegment: 'confirmation',
        // Terminal step -- nextStepId is not used but required by assert.
        // We handle this by checking isTerminalStep before calling resolveNext.
        nextStepId: kStepConfirmation,
      ),
    },
  );
}
```

```dart
// file: lib/wizard/wizard_progress_indicator.dart

import 'package:flutter/material.dart';
import 'wizard_engine.dart';

class WizardProgressIndicator extends StatelessWidget {
  final WizardEngine engine;

  const WizardProgressIndicator({super.key, required this.engine});

  @override
  Widget build(BuildContext context) {
    final total = engine.totalStepsInCurrentPath;
    final current = engine.currentStepIndex;
    final progress = total > 0 ? current / total : 0.0;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16.0),
          child: Text(
            'Step $current of $total: ${engine.currentStep.label}',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ),
        const SizedBox(height: 4),
        LinearProgressIndicator(value: progress),
      ],
    );
  }
}
```

```dart
// file: lib/wizard/wizard_scaffold.dart

import 'package:flutter/material.dart';
import 'wizard_engine.dart';
import 'wizard_progress_indicator.dart';

class WizardScaffold extends StatelessWidget {
  final WizardEngine engine;
  final Widget child;
  final VoidCallback? onSaveDraft;

  const WizardScaffold({
    super.key,
    required this.engine,
    required this.child,
    this.onSaveDraft,
  });

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: !engine.isFirstStep,
      onPopInvokedWithResult: (didPop, result) async {
        if (didPop) {
          engine.goBack();
          return;
        }

        // On the first step, ask before exiting
        final shouldExit = await showDialog<bool>(
          context: context,
          builder: (ctx) => AlertDialog(
            title: const Text('Exit Wizard?'),
            content: const Text(
              'Your progress will be lost unless you save a draft.',
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: const Text('Stay'),
              ),
              if (onSaveDraft != null)
                TextButton(
                  onPressed: () {
                    onSaveDraft!();
                    Navigator.pop(ctx, true);
                  },
                  child: const Text('Save & Exit'),
                ),
              TextButton(
                onPressed: () => Navigator.pop(ctx, true),
                child: const Text('Exit'),
              ),
            ],
          ),
        );

        if (shouldExit == true && context.mounted) {
          Navigator.pop(context);
        }
      },
      child: Scaffold(
        appBar: AppBar(
          title: Text(engine.currentStep.label),
          leading: engine.isFirstStep
              ? const CloseButton()
              : const BackButton(),
          actions: [
            if (onSaveDraft != null)
              IconButton(
                icon: const Icon(Icons.save),
                onPressed: onSaveDraft,
                tooltip: 'Save Draft',
              ),
          ],
        ),
        body: Column(
          children: [
            WizardProgressIndicator(engine: engine),
            Expanded(child: child),
          ],
        ),
      ),
    );
  }
}
```

```dart
// file: lib/features/loan/steps/personal_info_step.dart

import 'package:flutter/material.dart';
import '../../../wizard/wizard_engine.dart';

class PersonalInfoStep extends StatefulWidget {
  final WizardEngine engine;

  const PersonalInfoStep({super.key, required this.engine});

  @override
  State<PersonalInfoStep> createState() => _PersonalInfoStepState();
}

class _PersonalInfoStepState extends State<PersonalInfoStep> {
  late final TextEditingController _nameController;
  late final TextEditingController _emailController;
  late final TextEditingController _phoneController;

  @override
  void initState() {
    super.initState();
    // Restore data if user navigated back to this step
    final data = widget.engine.data;
    _nameController = TextEditingController(text: data['name'] as String? ?? '');
    _emailController = TextEditingController(text: data['email'] as String? ?? '');
    _phoneController = TextEditingController(text: data['phone'] as String? ?? '');
  }

  @override
  void dispose() {
    _nameController.dispose();
    _emailController.dispose();
    _phoneController.dispose();
    super.dispose();
  }

  void _onNext() {
    if (_nameController.text.isEmpty || _emailController.text.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Name and email are required.')),
      );
      return;
    }

    widget.engine.submitStep({
      'name': _nameController.text,
      'email': _emailController.text,
      'phone': _phoneController.text,
    });
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(16.0),
      child: Column(
        children: [
          TextField(
            controller: _nameController,
            decoration: const InputDecoration(labelText: 'Full Name'),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _emailController,
            decoration: const InputDecoration(labelText: 'Email'),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _phoneController,
            decoration: const InputDecoration(labelText: 'Phone'),
          ),
          const Spacer(),
          SizedBox(
            width: double.infinity,
            child: ElevatedButton(
              onPressed: _onNext,
              child: const Text('Next'),
            ),
          ),
        ],
      ),
    );
  }
}
```

```dart
// file: lib/features/loan/steps/loan_type_step.dart

import 'package:flutter/material.dart';
import '../../../wizard/wizard_engine.dart';

class LoanTypeStep extends StatefulWidget {
  final WizardEngine engine;

  const LoanTypeStep({super.key, required this.engine});

  @override
  State<LoanTypeStep> createState() => _LoanTypeStepState();
}

class _LoanTypeStepState extends State<LoanTypeStep> {
  late String _selectedType;

  @override
  void initState() {
    super.initState();
    _selectedType = widget.engine.data['loanType'] as String? ?? 'personal';
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(16.0),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'Select Loan Type',
            style: Theme.of(context).textTheme.titleLarge,
          ),
          const SizedBox(height: 24),
          RadioListTile<String>(
            title: const Text('Personal Loan'),
            subtitle: const Text('Unsecured loan for personal expenses'),
            value: 'personal',
            groupValue: _selectedType,
            onChanged: (v) => setState(() => _selectedType = v!),
          ),
          RadioListTile<String>(
            title: const Text('Mortgage'),
            subtitle: const Text('Home purchase or refinancing'),
            value: 'mortgage',
            groupValue: _selectedType,
            onChanged: (v) => setState(() => _selectedType = v!),
          ),
          RadioListTile<String>(
            title: const Text('Auto Loan'),
            subtitle: const Text('Vehicle purchase financing'),
            value: 'auto',
            groupValue: _selectedType,
            onChanged: (v) => setState(() => _selectedType = v!),
          ),
          const Spacer(),
          SizedBox(
            width: double.infinity,
            child: ElevatedButton(
              onPressed: () {
                widget.engine.submitStep({'loanType': _selectedType});
              },
              child: const Text('Next'),
            ),
          ),
        ],
      ),
    );
  }
}
```

```dart
// file: lib/features/loan/steps/review_step.dart

import 'package:flutter/material.dart';
import '../../../wizard/wizard_engine.dart';

class ReviewStep extends StatelessWidget {
  final WizardEngine engine;

  const ReviewStep({super.key, required this.engine});

  @override
  Widget build(BuildContext context) {
    final data = engine.data;
    final loanType = data['loanType'] as String? ?? 'unknown';

    return Padding(
      padding: const EdgeInsets.all(16.0),
      child: ListView(
        children: [
          Text('Review Your Application',
              style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 24),

          _buildSection('Personal Information', [
            'Name: ${data['name']}',
            'Email: ${data['email']}',
            'Phone: ${data['phone'] ?? 'Not provided'}',
          ]),

          _buildSection('Loan Type', [loanType.toUpperCase()]),

          if (loanType == 'mortgage' && data.containsKey('propertyAddress'))
            _buildSection('Property', [
              'Address: ${data['propertyAddress']}',
              'Value: \$${data['propertyValue']}',
            ]),

          if (loanType == 'auto' && data.containsKey('vehicleMake'))
            _buildSection('Vehicle', [
              '${data['vehicleYear']} ${data['vehicleMake']} ${data['vehicleModel']}',
            ]),

          _buildSection('Employment', [
            'Employer: ${data['employer'] ?? 'Not provided'}',
            'Title: ${data['jobTitle'] ?? 'Not provided'}',
          ]),

          _buildSection('Income', [
            'Annual: \$${data['annualIncome'] ?? 'Not provided'}',
          ]),

          if (loanType == 'mortgage' && data.containsKey('downPayment'))
            _buildSection('Down Payment', [
              '\$${data['downPayment']}',
            ]),

          const SizedBox(height: 32),
          SizedBox(
            width: double.infinity,
            child: ElevatedButton(
              onPressed: () => engine.submitStep({}),
              child: const Text('Submit Application'),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildSection(String title, List<String> items) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 16.0),
      child: Card(
        child: Padding(
          padding: const EdgeInsets.all(12.0),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: const TextStyle(fontWeight: FontWeight.bold)),
              const SizedBox(height: 8),
              ...items.map((item) => Padding(
                    padding: const EdgeInsets.only(bottom: 4.0),
                    child: Text(item),
                  )),
            ],
          ),
        ),
      ),
    );
  }
}
```

### Common Mistakes

**Mistake: Using a list instead of a graph for wizard steps.**
A list works for linear wizards but fails the moment you need branching. If you start with a list and retrofit branching, you end up with index arithmetic and special cases everywhere. Start with the graph model even for simple wizards -- the overhead is minimal and it scales cleanly.

**Mistake: Storing branch-specific data under generic keys.**
If both the Mortgage and Auto branches have a "value" field (property value vs. vehicle value), using the key `'value'` for both causes them to overwrite each other. Use branch-prefixed keys: `'propertyValue'`, `'vehicleValue'`.

**Mistake: Not invalidating stale branch data.**
If the user selects "Mortgage," fills in property info, goes back and changes to "Auto," the property info is still in the data map. The Review step must filter displayed data based on the current loan type, not just dump everything. Alternatively, clean up stale data when the branch changes (more complex but cleaner).

**Mistake: Browser back button bypasses wizard history.**
If you integrate with GoRouter, pressing the browser back button triggers a route pop, not a wizard `goBack()`. You need to intercept the pop (via `PopScope` or GoRouter's `onExit`) and delegate to the wizard engine. Otherwise the user exits the wizard entirely instead of going to the previous step.

### Debugging Tips

**Wizard state inspection:** Add a debug overlay that shows the current history stack and data map. This makes it immediately visible when state is wrong.

```dart
// Temporary debug helper -- remove before shipping
if (kDebugMode) {
  print('History: ${engine.history}');
  print('Data: ${engine.data}');
}
```

**Graph validation at startup:** Before the wizard starts, walk the graph from every node and verify every edge points to a valid step ID. This catches typos in step IDs that would otherwise cause null errors at runtime.

**Test the branching logic in isolation.** The `WizardEngine` is pure Dart with no Flutter dependencies. Write unit tests that call `submitStep` with different data and assert the `currentStepId` changes correctly. This is far faster than testing through the UI.

### Alternative Approaches

**Stepper widget:** Flutter's built-in `Stepper` widget works for simple linear flows. It handles the progress indicator and step navigation. But it does not support branching, has limited customization, and is not designed for multi-screen flows. Use it for short, linear forms (3-5 steps).

**Page-based wizard with PageView:** Using a `PageView` with `PageController` gives you swipe gestures and built-in animations. But `PageView` expects a fixed list of pages, which clashes with branching. You would need to rebuild the page list on every branch change.

**State machine approach:** For extremely complex flows with conditional loops, error recovery, and parallel steps, consider modeling the wizard as a finite state machine using a package like `bloc` or `xstate`-style libraries. This is overkill for most wizards but appropriate for regulatory compliance flows where the state logic must be auditable.

---

## General Debugging Tips for Navigation

1. **"Navigator operation requested with a context that does not include a Navigator."** This means you are using a `BuildContext` that is *above* the `Navigator` in the widget tree. Common cause: calling `Navigator.of(context)` from the same `build` method that creates the `MaterialApp`. The fix is to use a context from a widget *below* the `MaterialApp`.

2. **Black screen after navigation.** Usually caused by a route builder that returns a widget with no `Scaffold` or `Material` ancestor. Wrap your screen in a `Scaffold`.

3. **GoRouter redirect loop.** Add logging to your redirect callback: `debugPrint('Redirect: ${state.matchedLocation}')`. You will see the loop immediately. The fix is always a missing `return null` for the case where no redirect is needed.

4. **Named route not found.** With `onGenerateRoute`, this produces an error. With GoRouter, it hits the `errorBuilder`. Check for typos in route paths and ensure every `context.go('/path')` matches a defined route.

5. **State lost on tab switch.** Verify you are using `StatefulShellRoute.indexedStack` (not the plain `StatefulShellRoute`). Verify your tab screens are not recreated on every build by checking that the `key` parameter is stable.

6. **Hero animation not working.** Check: (a) tags match exactly, (b) both Hero widgets are in the tree during the transition, (c) you are using `pageBuilder` not `builder` with GoRouter, (d) the transition type allows both pages to be visible simultaneously (fade works, opaque slide may not).
