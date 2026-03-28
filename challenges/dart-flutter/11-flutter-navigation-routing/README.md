# Section 11: Flutter Navigation & Routing

## Introduction

Every non-trivial app has more than one screen. Navigation is the backbone that connects those screens into a coherent experience -- it determines how users move through your app, how deep links restore specific states, and how the browser's back button behaves on web. Getting navigation wrong leads to lost state, broken back buttons, and confused users.

Flutter offers two navigation paradigms. The **imperative** approach (Navigator 1.0) tells the framework exactly what to do: "push this screen," "pop back." It is simple and intuitive for straightforward flows. The **declarative** approach (Navigator 2.0 and packages like GoRouter) describes what the navigation state *should be*, and the framework figures out the transitions. Declarative routing shines in apps with deep linking, web URLs, and complex navigation trees.

This section starts with the imperative basics, moves through named routes, introduces GoRouter for declarative routing, and builds up to nested navigation, route guards, and custom transitions. By the end you will be comfortable choosing the right navigation strategy for any scenario.

## Prerequisites

- **Section 09 (Flutter Setup & Widgets):** You need working knowledge of StatelessWidget, StatefulWidget, BuildContext, and the widget tree.
- **Section 10 (Flutter Layouts):** You should be comfortable with Scaffold, AppBar, Column, Row, and Container to build the screens we will navigate between.
- A Flutter project you can run on an emulator, device, or Chrome (for web-specific exercises).

## Learning Objectives

By the end of this section you will be able to:

1. **Explain** the difference between imperative and declarative navigation and when each is appropriate.
2. **Implement** multi-screen navigation using Navigator.push, pop, pushReplacement, and pushAndRemoveUntil.
3. **Configure** named routes with onGenerateRoute and pass typed arguments between screens.
4. **Set up** GoRouter with path parameters, query parameters, nested routes, and redirects.
5. **Design** route guards that handle authentication redirects and role-based access control.
6. **Build** nested navigation patterns with bottom navigation bars and independent back stacks.
7. **Create** custom page transitions and hero animations between routes.
8. **Handle** deep linking configuration on Android and iOS, and manage browser history on web.

---

## Core Concepts

### 1. Navigator 1.0 -- The Imperative Model

Navigator 1.0 treats navigation as a stack of routes. You push screens onto the stack and pop them off. The framework manages the back button for you.

**Why a stack?** Because navigation is fundamentally last-in-first-out. The screen you most recently visited is the first one you go back to. This mental model works well for linear flows.

```dart
// file: lib/screens/home_screen.dart

import 'package:flutter/material.dart';
import 'detail_screen.dart';

class HomeScreen extends StatelessWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Home')),
      body: Center(
        child: ElevatedButton(
          onPressed: () {
            // Push a new route onto the stack
            Navigator.push(
              context,
              MaterialPageRoute(
                builder: (context) => const DetailScreen(itemId: 42),
              ),
            );
          },
          child: const Text('View Item 42'),
        ),
      ),
    );
  }
}
```

```dart
// file: lib/screens/detail_screen.dart

import 'package:flutter/material.dart';

class DetailScreen extends StatelessWidget {
  final int itemId;

  const DetailScreen({super.key, required this.itemId});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('Item $itemId')),
      body: Center(
        child: ElevatedButton(
          onPressed: () {
            // Pop the current route off the stack
            Navigator.pop(context);
          },
          child: const Text('Go Back'),
        ),
      ),
    );
  }
}
```

**pushReplacement** removes the current route and replaces it. This is critical for login flows -- after the user authenticates, you replace the login screen so they cannot press back to return to it.

```dart
// file: lib/screens/login_screen.dart

void onLoginSuccess(BuildContext context) {
  Navigator.pushReplacement(
    context,
    MaterialPageRoute(builder: (context) => const DashboardScreen()),
  );
}
```

**pushAndRemoveUntil** clears the stack down to a condition. Use it for logout flows where you want to return to the root screen and discard everything else.

```dart
// file: lib/navigation/navigation_helpers.dart

void navigateToHomeAndClearStack(BuildContext context) {
  Navigator.pushAndRemoveUntil(
    context,
    MaterialPageRoute(builder: (context) => const HomeScreen()),
    (route) => false, // Remove all previous routes
  );
}
```

### 2. Named Routes and onGenerateRoute

Hardcoding screen constructors inside Navigator.push calls creates tight coupling. Named routes decouple the navigation call from the screen instantiation.

```dart
// file: lib/main.dart

import 'package:flutter/material.dart';
import 'screens/home_screen.dart';
import 'screens/detail_screen.dart';
import 'screens/profile_screen.dart';

void main() => runApp(const MyApp());

class MyApp extends StatelessWidget {
  const MyApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      initialRoute: '/',
      onGenerateRoute: (RouteSettings settings) {
        switch (settings.name) {
          case '/':
            return MaterialPageRoute(
              builder: (_) => const HomeScreen(),
              settings: settings,
            );
          case '/detail':
            final args = settings.arguments as Map<String, dynamic>;
            return MaterialPageRoute(
              builder: (_) => DetailScreen(itemId: args['id'] as int),
              settings: settings,
            );
          case '/profile':
            return MaterialPageRoute(
              builder: (_) => const ProfileScreen(),
              settings: settings,
            );
          default:
            return MaterialPageRoute(
              builder: (_) => const NotFoundScreen(),
              settings: settings,
            );
        }
      },
    );
  }
}
```

Now navigation becomes:

```dart
// file: lib/screens/home_screen.dart (navigation call)

Navigator.pushNamed(
  context,
  '/detail',
  arguments: {'id': 42},
);
```

**Why onGenerateRoute over the routes map?** The simple `routes:` parameter cannot accept arguments. `onGenerateRoute` gives you access to `RouteSettings` where you can extract and validate arguments, handle unknown routes, and apply middleware logic before building the screen.

### 3. GoRouter -- Declarative Routing

GoRouter brings declarative, URL-based routing to Flutter. It is the recommended approach for apps that need deep linking, web URL support, or complex route hierarchies.

**Why GoRouter instead of Navigator 2.0 directly?** Navigator 2.0's raw API (Router, RouteInformationParser, RouterDelegate) requires hundreds of lines of boilerplate. GoRouter wraps that complexity in a clean, configuration-driven API.

```dart
// file: lib/router/app_router.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../screens/home_screen.dart';
import '../screens/detail_screen.dart';
import '../screens/settings_screen.dart';

final GoRouter appRouter = GoRouter(
  initialLocation: '/',
  routes: [
    GoRoute(
      path: '/',
      name: 'home',
      builder: (context, state) => const HomeScreen(),
      routes: [
        // Nested route: /detail/:id
        GoRoute(
          path: 'detail/:id',
          name: 'detail',
          builder: (context, state) {
            final itemId = int.parse(state.pathParameters['id']!);
            return DetailScreen(itemId: itemId);
          },
        ),
      ],
    ),
    GoRoute(
      path: '/settings',
      name: 'settings',
      builder: (context, state) {
        final tab = state.uri.queryParameters['tab'] ?? 'general';
        return SettingsScreen(initialTab: tab);
      },
    ),
  ],
  errorBuilder: (context, state) => const NotFoundScreen(),
);
```

```dart
// file: lib/main.dart (GoRouter setup)

import 'package:flutter/material.dart';
import 'router/app_router.dart';

void main() => runApp(const MyApp());

class MyApp extends StatelessWidget {
  const MyApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      routerConfig: appRouter,
    );
  }
}
```

Navigation with GoRouter uses `context.go()` for declarative navigation (replaces the stack) or `context.push()` for imperative-style pushes:

```dart
// file: lib/screens/home_screen.dart (GoRouter navigation)

// Declarative: sets the location, adjusting the stack accordingly
context.go('/detail/42');

// With query parameters
context.go('/settings?tab=notifications');

// Imperative push (adds to stack)
context.push('/detail/42');

// Named route navigation
context.goNamed('detail', pathParameters: {'id': '42'});
```

### 4. Route Guards and Redirects

Almost every app needs to protect certain routes behind authentication. GoRouter handles this with the `redirect` callback.

```dart
// file: lib/router/app_router.dart (with guards)

final GoRouter appRouter = GoRouter(
  initialLocation: '/',
  redirect: (BuildContext context, GoRouterState state) {
    final authService = AuthService.instance;
    final isLoggedIn = authService.isAuthenticated;
    final isGoingToLogin = state.matchedLocation == '/login';

    // Not logged in and not heading to login -> redirect to login
    if (!isLoggedIn && !isGoingToLogin) {
      return '/login?redirect=${state.matchedLocation}';
    }

    // Logged in but heading to login -> redirect to home
    if (isLoggedIn && isGoingToLogin) {
      return '/';
    }

    // No redirect needed
    return null;
  },
  routes: [
    // ... route definitions
  ],
);
```

For role-based access, check the user's role inside route-level redirects:

```dart
// file: lib/router/admin_routes.dart

GoRoute(
  path: '/admin',
  redirect: (context, state) {
    final user = AuthService.instance.currentUser;
    if (user == null) return '/login';
    if (user.role != UserRole.admin) return '/unauthorized';
    return null;
  },
  builder: (context, state) => const AdminDashboard(),
),
```

### 5. Nested Navigation and Bottom Navigation Bars

Apps with bottom navigation bars need independent navigation stacks for each tab. Tapping a tab should not destroy the scroll position or state of other tabs.

GoRouter supports this with `StatefulShellRoute`:

```dart
// file: lib/router/shell_routes.dart

import 'package:go_router/go_router.dart';
import '../shells/main_shell.dart';

final routerConfig = GoRouter(
  routes: [
    StatefulShellRoute.indexedStack(
      builder: (context, state, navigationShell) {
        return MainShell(navigationShell: navigationShell);
      },
      branches: [
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/',
              builder: (context, state) => const HomeTab(),
              routes: [
                GoRoute(
                  path: 'detail/:id',
                  builder: (context, state) => DetailScreen(
                    itemId: int.parse(state.pathParameters['id']!),
                  ),
                ),
              ],
            ),
          ],
        ),
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/search',
              builder: (context, state) => const SearchTab(),
            ),
          ],
        ),
        StatefulShellBranch(
          routes: [
            GoRoute(
              path: '/profile',
              builder: (context, state) => const ProfileTab(),
            ),
          ],
        ),
      ],
    ),
  ],
);
```

```dart
// file: lib/shells/main_shell.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

class MainShell extends StatelessWidget {
  final StatefulNavigationShell navigationShell;

  const MainShell({super.key, required this.navigationShell});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: navigationShell,
      bottomNavigationBar: NavigationBar(
        selectedIndex: navigationShell.currentIndex,
        onDestinationSelected: (index) {
          navigationShell.goBranch(
            index,
            initialLocation: index == navigationShell.currentIndex,
          );
        },
        destinations: const [
          NavigationDestination(icon: Icon(Icons.home), label: 'Home'),
          NavigationDestination(icon: Icon(Icons.search), label: 'Search'),
          NavigationDestination(icon: Icon(Icons.person), label: 'Profile'),
        ],
      ),
    );
  }
}
```

### 6. Custom Page Transitions

Default transitions (slide on iOS, fade on Android) are fine, but sometimes your design calls for something different. GoRouter accepts a `pageBuilder` that gives you full control.

```dart
// file: lib/router/transitions.dart

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

CustomTransitionPage buildFadeTransition({
  required GoRouterState state,
  required Widget child,
}) {
  return CustomTransitionPage(
    key: state.pageKey,
    child: child,
    transitionsBuilder: (context, animation, secondaryAnimation, child) {
      return FadeTransition(opacity: animation, child: child);
    },
  );
}

CustomTransitionPage buildSlideUpTransition({
  required GoRouterState state,
  required Widget child,
}) {
  return CustomTransitionPage(
    key: state.pageKey,
    child: child,
    transitionDuration: const Duration(milliseconds: 400),
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
}
```

Use it in a route definition:

```dart
// file: lib/router/app_router.dart (transition usage)

GoRoute(
  path: 'detail/:id',
  pageBuilder: (context, state) => buildSlideUpTransition(
    state: state,
    child: DetailScreen(itemId: int.parse(state.pathParameters['id']!)),
  ),
),
```

### 7. Hero Animations Between Routes

Hero animations create visual continuity when an element "flies" from one screen to another. Wrap the same logical element in both screens with matching `tag` values.

```dart
// file: lib/screens/gallery_screen.dart

Hero(
  tag: 'image-${item.id}',
  child: Image.network(item.thumbnailUrl, width: 100, height: 100),
)
```

```dart
// file: lib/screens/image_detail_screen.dart

Hero(
  tag: 'image-${widget.itemId}',
  child: Image.network(widget.fullImageUrl),
)
```

The tags must match exactly. If they do not, you get no animation and no error -- a silent failure that is easy to miss.

### 8. Back Button Handling with PopScope

`WillPopScope` is deprecated as of Flutter 3.12. Use `PopScope` instead.

```dart
// file: lib/screens/form_screen.dart

class FormScreen extends StatefulWidget {
  const FormScreen({super.key});

  @override
  State<FormScreen> createState() => _FormScreenState();
}

class _FormScreenState extends State<FormScreen> {
  bool _hasUnsavedChanges = false;

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: !_hasUnsavedChanges,
      onPopInvokedWithResult: (didPop, result) async {
        if (didPop) return;

        final shouldLeave = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('Unsaved Changes'),
            content: const Text('You have unsaved changes. Leave anyway?'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('Stay'),
              ),
              TextButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('Leave'),
              ),
            ],
          ),
        );

        if (shouldLeave == true && context.mounted) {
          Navigator.pop(context);
        }
      },
      child: Scaffold(
        appBar: AppBar(title: const Text('Edit Form')),
        body: TextField(
          onChanged: (_) => setState(() => _hasUnsavedChanges = true),
        ),
      ),
    );
  }
}
```

### 9. Deep Linking

Deep linking allows external URLs to open specific screens in your app. On Android, configure `AndroidManifest.xml`. On iOS, configure `Info.plist` and the associated domains entitlement.

```xml
<!-- file: android/app/src/main/AndroidManifest.xml (inside <activity>) -->

<intent-filter android:autoVerify="true">
  <action android:name="android.intent.action.VIEW" />
  <category android:name="android.intent.category.DEFAULT" />
  <category android:name="android.intent.category.BROWSABLE" />
  <data android:scheme="https" android:host="myapp.example.com" />
</intent-filter>
```

GoRouter automatically handles incoming deep link URLs and matches them against your route configuration. The key insight is that your route paths double as your deep link paths. If a user opens `https://myapp.example.com/detail/42`, GoRouter navigates to your `/detail/42` route.

### 10. Navigation Patterns: Dialogs, Sheets, and Drawers

Not all navigation involves full-screen transitions. Modal bottom sheets, dialogs, and drawers are navigation events too.

```dart
// file: lib/screens/home_screen.dart (modal patterns)

// Modal bottom sheet
void showOptionsSheet(BuildContext context) {
  showModalBottomSheet(
    context: context,
    isScrollControlled: true,
    builder: (context) => DraggableScrollableSheet(
      initialChildSize: 0.5,
      minChildSize: 0.25,
      maxChildSize: 0.9,
      expand: false,
      builder: (context, scrollController) {
        return ListView(
          controller: scrollController,
          children: const [
            ListTile(title: Text('Option A')),
            ListTile(title: Text('Option B')),
            ListTile(title: Text('Option C')),
          ],
        );
      },
    ),
  );
}

// Dialog navigation
Future<bool?> showConfirmDialog(BuildContext context, String message) {
  return showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      content: Text(message),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: const Text('Cancel'),
        ),
        TextButton(
          onPressed: () => Navigator.pop(context, true),
          child: const Text('Confirm'),
        ),
      ],
    ),
  );
}
```

---

## Exercises

### Exercise 1 (Basic): Multi-Screen Navigation with Data Passing

**Goal:** Build a three-screen app (Product List, Product Detail, Cart) using Navigator.push and Navigator.pop, passing data between screens via constructors.

**Instructions:**

1. Create a `Product` model class with fields: `id`, `name`, `price`, `description`.
2. Build a `ProductListScreen` that displays a list of at least 5 hardcoded products.
3. Tapping a product should `Navigator.push` to `ProductDetailScreen`, passing the `Product` object via constructor.
4. `ProductDetailScreen` displays all product details and has an "Add to Cart" button.
5. The "Add to Cart" button should `Navigator.pop` back to the list and return the product as a result.
6. Capture the returned product from `Navigator.pop` and show a SnackBar confirming it was added.

**Verification:**
- Tap a product, verify the detail screen shows correct data.
- Press the back arrow -- no SnackBar should appear (the user did not add to cart).
- Tap "Add to Cart" -- verify you return to the list and the SnackBar appears with the product name.
- Verify the Android back button behaves the same as the AppBar back arrow.

```dart
// file: lib/models/product.dart
// Define the Product class here

// file: lib/screens/product_list_screen.dart
// Build the list and handle the result from Navigator.push

// file: lib/screens/product_detail_screen.dart
// Display product data and pop with a result
```

---

### Exercise 2 (Basic): Named Routes with onGenerateRoute

**Goal:** Refactor the product app from Exercise 1 to use named routes with argument passing through `RouteSettings`.

**Instructions:**

1. Define three named routes: `/`, `/product`, and `/cart`.
2. Implement `onGenerateRoute` in your `MaterialApp` to handle all three routes.
3. Pass the `Product` object as an argument through `settings.arguments`.
4. Add type-checking on the arguments: if someone navigates to `/product` without a valid `Product` argument, show an error screen.
5. Add a fallback route for unknown paths that displays a "404 Not Found" screen.

**Verification:**
- Navigation should work identically to Exercise 1.
- Deliberately navigate to an undefined route (e.g., `/nonexistent`) and verify the 404 screen appears.
- Try navigating to `/product` without arguments and verify the error screen appears.

```dart
// file: lib/main.dart
// Implement onGenerateRoute with argument validation

// file: lib/screens/error_screen.dart
// Handle missing or invalid arguments

// file: lib/screens/not_found_screen.dart
// Display for unknown routes
```

---

### Exercise 3 (Intermediate): GoRouter Setup with Nested Routes

**Goal:** Build a blog reader app using GoRouter with path parameters, query parameters, and nested routes.

**Instructions:**

1. Add `go_router` to your `pubspec.yaml`.
2. Define these routes:
   - `/` -- Home screen showing a list of blog categories.
   - `/category/:categoryId` -- List of posts in a category.
   - `/category/:categoryId/post/:postId` -- Individual post detail (nested under category).
   - `/search?q=<query>&sort=<sort>` -- Search screen with query parameters.
3. Create mock data: at least 3 categories with 3 posts each.
4. Implement a proper error page for invalid category or post IDs.
5. Use `context.go()` for top-level navigation and `context.push()` when you want to preserve the back stack.

**Verification:**
- Navigate from home to a category, then to a post. The back button should retrace your steps.
- Navigate directly to `/category/2/post/5` (simulate deep link by typing in the browser on web, or using `context.go()`). The screen should display correctly.
- Navigate to `/search?q=flutter&sort=newest` and verify both parameters are parsed.
- Navigate to `/category/999` (nonexistent) and verify the error page appears.

```dart
// file: lib/router/app_router.dart
// Define GoRouter configuration

// file: lib/data/mock_data.dart
// Blog categories and posts

// file: lib/screens/category_screen.dart
// Display posts for a given category ID

// file: lib/screens/post_detail_screen.dart
// Display a single post, extracting both categoryId and postId from path parameters
```

---

### Exercise 4 (Intermediate): Bottom Navigation with Separate Stacks

**Goal:** Build an app with bottom navigation (Home, Explore, Profile) where each tab maintains its own navigation stack.

**Instructions:**

1. Use GoRouter's `StatefulShellRoute.indexedStack` to create three branches.
2. Each branch must have at least two levels of depth (e.g., Home -> Item Detail, Explore -> Category -> Category Detail, Profile -> Settings).
3. Navigate deep into one tab, switch to another tab, then switch back -- the first tab's state must be preserved.
4. Implement "go to root on re-tap": tapping the already-selected tab should pop to the root of that branch.
5. The URL in the browser (when running on web) should reflect the current screen in the active tab.

**Verification:**
- Push two screens deep in the Home tab. Switch to Explore. Switch back to Home. You should still be two screens deep.
- Tap the Home tab icon while already on Home -- you should return to the root of the Home tab.
- On web, copy the URL showing a deep screen, open it in a new tab, and verify the correct screen loads.
- Press the Android back button from a nested screen and verify it pops within the tab, not between tabs.

```dart
// file: lib/router/shell_routes.dart
// StatefulShellRoute configuration

// file: lib/shells/app_shell.dart
// Scaffold with NavigationBar

// file: lib/screens/home/home_tab.dart
// file: lib/screens/explore/explore_tab.dart
// file: lib/screens/profile/profile_tab.dart
// Tab root screens with navigation to deeper screens
```

---

### Exercise 5 (Advanced): Route Guards with Async Authentication

**Goal:** Implement a complete authentication flow with route guards, including async token validation and role-based access.

**Instructions:**

1. Create a mock `AuthService` with methods: `login()`, `logout()`, `isAuthenticated` (getter), `currentUser` (getter with a `role` field).
2. Simulate an async authentication check: `validateToken()` returns a `Future<bool>` after a 500ms delay.
3. Configure GoRouter redirects:
   - Unauthenticated users trying to access any protected route get redirected to `/login?redirect=<intended_path>`.
   - After login, redirect to the path stored in the `redirect` query parameter.
   - Routes under `/admin` require `UserRole.admin`. Non-admin authenticated users see `/unauthorized`.
   - The `/login` route should redirect to `/` if the user is already authenticated.
4. Use `GoRouter.refreshListenable` to react to auth state changes (the router should re-evaluate redirects when the user logs in or out).
5. Add a splash screen that displays while the initial token validation runs.

**Verification:**
- Open the app without being logged in. Try navigating to `/dashboard` -- you should land on `/login?redirect=%2Fdashboard`.
- Log in. You should be redirected to `/dashboard` (the original intended destination).
- As a regular user, try navigating to `/admin` -- you should see the Unauthorized screen.
- Log out from any screen -- you should be redirected to `/login`.
- Refresh the app (hot restart). The splash screen should appear during token validation, then resolve to the correct screen.

```dart
// file: lib/services/auth_service.dart
// Mock auth service with ChangeNotifier

// file: lib/models/user.dart
// User model with role enum

// file: lib/router/app_router.dart
// GoRouter with redirect logic and refreshListenable

// file: lib/screens/splash_screen.dart
// Displayed during async auth check

// file: lib/screens/login_screen.dart
// Handles redirect query parameter after successful login
```

---

### Exercise 6 (Advanced): Custom Page Transitions and Hero Animations

**Goal:** Build an image gallery app with custom transitions: a grid-to-detail hero animation, a slide-up modal, and a fade transition for settings.

**Instructions:**

1. Create a `GalleryScreen` with a grid of image thumbnails (use `ColoredBox` or placeholder images).
2. Tapping an image opens `ImageDetailScreen` with a Hero animation. The image should smoothly expand from its grid position to full screen.
3. A floating action button opens an `EditScreen` with a slide-up-from-bottom transition (custom `transitionsBuilder`).
4. A settings icon in the AppBar opens `SettingsScreen` with a fade transition.
5. Make transitions configurable: create a reusable `TransitionType` enum and a factory that produces the correct `CustomTransitionPage`.
6. Ensure the Hero animation works correctly with GoRouter (you will need `pageBuilder` instead of `builder`).

**Verification:**
- Tap an image: it should visually "fly" from its grid cell to the detail screen. Press back and watch it fly back.
- Tap the FAB: the edit screen should slide up from the bottom. Dismiss it and watch it slide down.
- Tap settings: the screen should fade in. Go back and watch it fade out.
- Rapidly tap between screens. Transitions should not break or overlap.
- Test on both Android and iOS (or their emulators) to verify platform-appropriate behavior.

```dart
// file: lib/router/transitions.dart
// TransitionType enum and factory

// file: lib/screens/gallery_screen.dart
// Grid with Hero-wrapped thumbnails

// file: lib/screens/image_detail_screen.dart
// Full image with matching Hero tag

// file: lib/router/app_router.dart
// Routes using pageBuilder with different transition types
```

---

### Exercise 7 (Insane): Full Declarative Routing System

**Goal:** Build a comprehensive declarative routing system that supports nested navigation stacks, deep linking, route guards with async checks, custom transitions, and browser history support.

**Instructions:**

This is an integration challenge. You will wire together everything from this section into a single, production-quality app.

1. **App structure:** An e-commerce app with:
   - Bottom navigation: Shop, Cart, Orders, Account.
   - Shop tab: Category list -> Product list -> Product detail.
   - Cart tab: Cart items -> Checkout flow (3 steps: Address, Payment, Confirmation).
   - Orders tab: Order list -> Order detail.
   - Account tab: Profile -> Edit Profile, Settings, Saved Addresses.

2. **Route guards:**
   - Cart checkout requires authentication. Unauthenticated users get redirected to a login modal, then returned to checkout.
   - Order history requires authentication.
   - Account settings are accessible without auth (for app preferences), but profile editing requires auth.

3. **Deep linking:**
   - `/shop/category/:catId/product/:prodId` opens the product detail with correct shell context.
   - `/orders/:orderId` opens the order detail (redirecting to login first if needed).
   - Configure for both Android and iOS.

4. **Transitions:**
   - Shop navigation uses horizontal slide transitions.
   - Checkout flow uses a shared-element transition on the cart total.
   - Modals (login, confirmations) slide up from bottom.
   - Tab switches have no transition (instant swap).

5. **Browser history (web):**
   - Each navigation action updates the URL.
   - Forward/back browser buttons work correctly.
   - Refreshing the page restores the correct state (including which tab is active and how deep you are in that tab).

6. **State preservation:**
   - Switching tabs preserves scroll position and navigation depth.
   - The checkout flow preserves form data when navigating away and back (within reason).

**Verification:**
- Navigate through the entire Shop flow. Deep link to a product. The URL should be correct and the shell should show the Shop tab.
- Start the checkout flow, get redirected to login, log in, and verify you return to the exact checkout step.
- Open `/orders/42` in a browser while logged out. Log in. Verify you see order 42.
- On web, use browser back/forward through a complex navigation sequence (switch tabs, go deep, switch again). Every step should be consistent.
- Kill the app and reopen a deep link. The correct screen should appear.
- Switch between all four tabs rapidly. State must be preserved in each.

```dart
// file: lib/router/app_router.dart
// Full router configuration with StatefulShellRoute, guards, and transitions

// file: lib/router/route_guards.dart
// Authentication and role-based guard logic

// file: lib/router/transitions.dart
// Transition factory for different navigation contexts

// file: lib/shells/main_shell.dart
// Bottom navigation shell

// file: lib/services/auth_service.dart
// Auth state management with ChangeNotifier

// Implement all screens for Shop, Cart, Orders, and Account flows
```

---

### Exercise 8 (Insane): Multi-Step Wizard Engine with Branching Paths

**Goal:** Build a reusable wizard/multi-step flow engine that supports branching paths based on user input, state preservation across steps, and full back/forward navigation.

**Instructions:**

1. **Wizard engine (reusable):**
   - Create a `WizardRouter` class that manages a directed graph of steps.
   - Each step is a widget that reports its result to the engine.
   - The engine determines the next step based on the result (branching logic).
   - Support linear sequences, conditional branches, and convergence points (branches that merge back).
   - Maintain a history stack for back navigation that respects the actual path taken (not the graph structure).

2. **Use case -- Loan Application Wizard:**
   - Step 1: Personal Info (name, email, phone).
   - Step 2: Loan Type selection (Personal, Mortgage, Auto).
   - Branch A (Personal): Employment info -> Income details -> Review.
   - Branch B (Mortgage): Property info -> Employment info -> Income details -> Down payment -> Review.
   - Branch C (Auto): Vehicle info -> Employment info -> Income details -> Review.
   - Review step: Shows all collected data from the taken path.
   - Confirmation step (common endpoint for all branches).

3. **State preservation:**
   - All form data is preserved when navigating back and forward.
   - If the user changes a branching decision (e.g., switches from Personal to Mortgage), the engine discards the obsolete branch data but preserves shared step data (like Employment info if already filled).

4. **Navigation features:**
   - Progress indicator that shows the correct total steps for the current path.
   - "Back" navigates to the previous step in the user's history.
   - The Android back button triggers "Back" within the wizard, and shows a confirmation dialog when on step 1.
   - A "Save Draft" feature that serializes the wizard state to JSON.
   - A "Resume Draft" feature that deserializes and restores the wizard to the exact step and path.

5. **URL integration (web):**
   - Each wizard step updates the URL: `/apply/personal-info`, `/apply/mortgage/property-info`.
   - Browser back/forward navigate within the wizard.
   - Refreshing the page mid-wizard restores the state (from in-memory or query parameter).

**Verification:**
- Complete the Personal loan path end to end. Review screen should show only Personal-relevant data.
- Go back to Step 2, change to Mortgage. Proceed through Mortgage-specific steps. Employment info (shared step) should retain your previous data.
- On the Mortgage Review screen, verify the data includes Property info and Down payment but NOT Vehicle info.
- Save a draft mid-wizard. Restart the app. Resume the draft. You should be on the exact same step with all data intact.
- On web, use browser back/forward through the wizard steps. The URL, displayed step, and form data should all stay in sync.
- Press the Android back button on Step 1. A confirmation dialog should appear asking if you want to exit the wizard.

```dart
// file: lib/wizard/wizard_engine.dart
// Core engine: step graph, branching logic, history stack

// file: lib/wizard/wizard_state.dart
// Serializable state container for all form data

// file: lib/wizard/wizard_router_integration.dart
// Integration with GoRouter for URL sync

// file: lib/wizard/wizard_progress_indicator.dart
// Dynamic progress bar based on current path length

// file: lib/features/loan/loan_wizard_config.dart
// Step definitions and branching rules for the loan application

// file: lib/features/loan/steps/personal_info_step.dart
// file: lib/features/loan/steps/loan_type_step.dart
// file: lib/features/loan/steps/review_step.dart
// Individual step widgets
```

---

## Summary

Navigation in Flutter ranges from the simple `Navigator.push`/`pop` to fully declarative systems with GoRouter. The right choice depends on your app's complexity:

- **Small apps with linear flows:** Navigator 1.0 with `push` and `pop` is perfectly fine. Do not over-engineer.
- **Apps needing deep linking or web support:** GoRouter with declarative routes. The URL-first approach pays off quickly.
- **Apps with authentication flows:** Route guards via GoRouter's `redirect` callback, combined with `refreshListenable` for reactive auth state.
- **Apps with bottom navigation:** `StatefulShellRoute.indexedStack` preserves tab state without manual management.
- **Complex multi-step flows:** Build a wizard engine on top of GoRouter, managing step state separately from navigation state.

The single most common navigation bug is using the wrong `BuildContext`. If you call `Navigator.of(context)` and the context belongs to a widget that is *above* the Navigator in the tree, the framework will not find it. Always verify which Navigator your context corresponds to.

## What's Next

**Section 12: State Management Basics** builds directly on navigation. Once you know how to move between screens, the immediate question becomes: how do you share and synchronize data *across* those screens? Section 12 introduces Provider, Riverpod, and the fundamentals of reactive state management that make your navigation flows data-aware.

## References

- [Flutter Navigation Cookbook](https://docs.flutter.dev/cookbook/navigation)
- [GoRouter Package](https://pub.dev/packages/go_router)
- [GoRouter Official Documentation](https://docs.flutter.dev/ui/navigation)
- [Navigator 2.0 Overview (Flutter.dev)](https://docs.flutter.dev/ui/navigation)
- [Deep Linking in Flutter](https://docs.flutter.dev/ui/navigation/deep-linking)
- [PopScope API Reference](https://api.flutter.dev/flutter/widgets/PopScope-class.html)
- [Hero Animations](https://docs.flutter.dev/ui/animations/hero-animations)
