# Section 13: Flutter Forms, User Input & Validation

## Introduction

Every meaningful application collects data from users. A social media app asks for your bio, a banking app needs transfer amounts, an e-commerce checkout demands addresses and payment details. Forms are the primary mechanism through which users communicate structured data back to your application, and how well you handle that process directly determines whether users trust your software or abandon it mid-flow.

Validation is not just error prevention -- it is user experience. A form that waits until submission to reveal five errors feels hostile. A form that gently guides users as they type, explains what it expects, and remembers their progress across steps feels like a conversation. Flutter gives you the building blocks for both approaches, and the architectural decisions you make here ripple through your entire application.

This section covers everything from basic text fields to complex multi-step wizards, from simple "required field" checks to asynchronous server-side validation with debouncing. You will learn not just the widgets, but the patterns that make forms maintainable as your application grows.

## Prerequisites

Before starting this section, you should be comfortable with:

- **Section 09 (Flutter Setup & Widgets):** Widget lifecycle, StatefulWidget, setState
- **Section 10 (Flutter Layouts):** Row, Column, Padding, Container for form layout
- **Section 11 (Navigation & Routing):** Pushing/popping routes (needed for multi-step forms)
- **Section 12 (State Basics):** State management fundamentals, lifting state up

## Learning Objectives

1. **Construct** forms using Form, TextFormField, and GlobalKey<FormState>
2. **Implement** synchronous and asynchronous validation with clear error messages
3. **Design** input decoration themes for consistent styling and accessible error states
4. **Manage** focus flow across fields using FocusNode and FocusScopeNode
5. **Apply** gesture detection to build interactive touch-based interfaces
6. **Integrate** selection widgets (Checkbox, Radio, Switch, Slider, Dropdown, DatePicker)
7. **Evaluate** reactive vs. traditional form patterns for a given scenario
8. **Create** dynamic, schema-driven form systems that render UI from data definitions

---

## Core Concepts

### TextField and TextEditingController

A TextEditingController owns the text value, cursor position, and selection state. Flutter separates it from the widget because the controller's lifetime often differs -- you might read its value from a button callback or pre-populate it from a database.

```dart
// file: text_field_basics.dart
class _SearchScreenState extends State<SearchScreen> {
  final _searchController = TextEditingController();

  @override
  void initState() {
    super.initState();
    _searchController.addListener(() {
      debugPrint('Query: ${_searchController.text}');
    });
  }

  @override
  void dispose() {
    // CRITICAL: Always dispose controllers to prevent memory leaks
    _searchController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return TextField(
      controller: _searchController,
      decoration: const InputDecoration(
        hintText: 'Search products...',
        prefixIcon: Icon(Icons.search),
      ),
      textInputAction: TextInputAction.search,
      onSubmitted: (value) => _performSearch(value),
    );
  }
}
```

### Form, TextFormField, and Validation

TextField works for standalone inputs, but coordinated validation across multiple fields requires the Form widget. TextFormField adds a `validator` callback and integrates with the parent Form's lifecycle.

```dart
// file: login_form.dart
class _LoginFormState extends State<LoginForm> {
  final _formKey = GlobalKey<FormState>();
  String _email = '';

  @override
  Widget build(BuildContext context) {
    return Form(
      key: _formKey,
      autovalidateMode: AutovalidateMode.onUserInteraction,
      child: Column(
        children: [
          TextFormField(
            decoration: const InputDecoration(labelText: 'Email'),
            keyboardType: TextInputType.emailAddress,
            validator: (value) {
              if (value == null || value.trim().isEmpty) return 'Email is required';
              if (!RegExp(r'^[\w\-.]+@([\w\-]+\.)+[\w\-]{2,4}$').hasMatch(value)) {
                return 'Enter a valid email address';
              }
              return null; // null means valid
            },
            onSaved: (value) => _email = value?.trim() ?? '',
          ),
          ElevatedButton(
            onPressed: () {
              if (!_formKey.currentState!.validate()) return;
              _formKey.currentState!.save(); // calls every onSaved
            },
            child: const Text('Submit'),
          ),
        ],
      ),
    );
  }
}
```

### Input Decoration Theming

Define decoration once in your theme to avoid scattered InputDecoration across dozens of fields.

```dart
// file: form_theme.dart
ThemeData buildAppTheme() {
  return ThemeData(
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Colors.grey.shade50,
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(8)),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(8),
        borderSide: const BorderSide(color: Colors.blue, width: 2),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(8),
        borderSide: const BorderSide(color: Colors.red),
      ),
    ),
  );
}
```

### Focus Management

Focus determines which widget receives keyboard input. Control focus flow so "Next" on the keyboard advances to the next field.

```dart
// file: focus_management.dart
class _ProfileFormState extends State<ProfileForm> {
  final _firstNameFocus = FocusNode();
  final _lastNameFocus = FocusNode();

  @override
  void dispose() {
    _firstNameFocus.dispose();
    _lastNameFocus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Column(children: [
      TextFormField(
        focusNode: _firstNameFocus,
        autofocus: true,
        textInputAction: TextInputAction.next,
        onFieldSubmitted: (_) => _lastNameFocus.requestFocus(),
      ),
      TextFormField(
        focusNode: _lastNameFocus,
        textInputAction: TextInputAction.done,
        onFieldSubmitted: (_) => _lastNameFocus.unfocus(),
      ),
    ]);
  }
}
```

### Keyboard Types and Input Formatters

Different data types deserve different keyboards. Input formatters constrain what characters can enter the field.

```dart
// file: input_formatters.dart
TextFormField(
  keyboardType: TextInputType.phone,
  inputFormatters: [
    FilteringTextInputFormatter.digitsOnly,
    LengthLimitingTextInputFormatter(10),
  ],
)

// Custom formatter: inserts dashes for phone numbers (555-123-4567)
class PhoneFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(TextEditingValue oldValue, TextEditingValue newValue) {
    final digits = newValue.text.replaceAll('-', '');
    final buffer = StringBuffer();
    for (var i = 0; i < digits.length; i++) {
      if (i == 3 || i == 6) buffer.write('-');
      buffer.write(digits[i]);
    }
    final formatted = buffer.toString();
    return TextEditingValue(
      text: formatted,
      selection: TextSelection.collapsed(offset: formatted.length),
    );
  }
}
```

### GestureDetector and Drag-and-Drop

GestureDetector responds to taps, long presses, drags, and multi-touch gestures. Draggable and DragTarget let users rearrange items; LongPressDraggable prevents accidental drags during scrolling.

```dart
// file: gesture_basics.dart
// Star rating built with GestureDetector
GestureDetector(
  onTap: () => setState(() => _rating = starNumber),
  onLongPress: () => debugPrint('Long pressed star $starNumber'),
  child: Icon(
    starNumber <= _rating ? Icons.star : Icons.star_border,
    color: Colors.amber,
    semanticLabel: 'Rate $starNumber of $maxStars',
  ),
)

// Drag and drop with LongPressDraggable + DragTarget
LongPressDraggable<String>(
  data: taskName,
  feedback: Material(elevation: 4, child: Text(taskName)),
  child: Card(child: ListTile(title: Text(taskName))),
)
DragTarget<String>(
  onAcceptWithDetails: (details) => moveTask(details.data),
  builder: (context, candidates, rejected) => Container(/*...*/),
)
```

### Selection Widgets

Flutter provides Checkbox, Radio, Switch, Slider, DropdownButton, DatePicker, and TimePicker. All share a consistent pattern: a current value plus a callback when the user changes it.

```dart
// file: selection_widgets.dart
SwitchListTile(value: _enabled, onChanged: (v) => setState(() => _enabled = v));

RadioListTile<String>(value: 'dark', groupValue: _theme, onChanged: (v) => setState(() => _theme = v!));

Slider(value: _fontSize, min: 10, max: 24, divisions: 14, onChanged: (v) => setState(() => _fontSize = v));

DropdownButtonFormField<String>(
  value: _lang,
  items: languages.map((l) => DropdownMenuItem(value: l, child: Text(l))).toList(),
  onChanged: (v) => setState(() => _lang = v),
  validator: (v) => v == null ? 'Please select a language' : null,
);

// Date picker launched from a tap
onTap: () async {
  final picked = await showDatePicker(
    context: context, initialDate: DateTime(2000),
    firstDate: DateTime(1900), lastDate: DateTime.now(),
  );
  if (picked != null) setState(() => _birthDate = picked);
}
```

### Debouncing and Async Validation

Some validations require a server round-trip. Debouncing waits until the user pauses typing before sending the request.

```dart
// file: async_validation.dart
Timer? _debounceTimer;

void _onUsernameChanged() {
  _debounceTimer?.cancel();
  if (_controller.text.length < 3) return;
  setState(() => _isChecking = true);
  _debounceTimer = Timer(const Duration(milliseconds: 500), () {
    _checkAvailability(_controller.text);
  });
}

Future<void> _checkAvailability(String username) async {
  await Future.delayed(const Duration(seconds: 1)); // simulate API
  if (!mounted) return;
  if (_controller.text.trim() != username) return; // text changed while waiting
  setState(() {
    _isChecking = false;
    _asyncError = ['admin', 'test'].contains(username.toLowerCase())
        ? '"$username" is already taken' : null;
  });
}
```

### Accessibility

Accessible forms are not optional. Screen readers need semantic labels, keyboard users need logical tab order, and motor-impaired users need 48x48 minimum touch targets.

```dart
// file: accessible_form.dart
Semantics(
  label: 'Email input field',
  hint: 'Enter your email address',
  textField: true,
  child: TextFormField(
    decoration: const InputDecoration(labelText: 'Email'),
    autofillHints: const [AutofillHints.email],
  ),
)
```

---

## Exercises

### Exercise 1 (Basic): Login Form with Validation

Build a `LoginScreen` with a Form containing email and password fields. Email validation: required, must match an email pattern. Password: required, minimum 8 characters, must contain at least one digit. Include a show/hide password toggle (suffixIcon), disable the submit button while submitting, and show a SnackBar on success. Use `AutovalidateMode.onUserInteraction`.

```dart
// file: exercise_1_starter.dart
class _LoginScreenState extends State<LoginScreen> {
  // TODO: Create form key, controllers, _obscurePassword toggle
  // TODO: Build Form with email/password TextFormFields
  // TODO: Implement _validateEmail, _validatePassword, _handleSubmit
}
```

**Verification:** Invalid email shows error after interaction. Password without digits shows specific error. Eye icon toggles visibility. Valid submit shows SnackBar.

### Exercise 2 (Basic): Contact Form with Multiple Input Types

Create a `ContactForm` with: name (text), email (text), category (DropdownButtonFormField with "Bug Report", "Feature Request", "Account Issue", "Other"), priority (Radio group: Low/Medium/High), message (multiline, 20-500 characters with live counter), subscribe (Checkbox). All except checkbox and priority are required. Include a Reset button that clears all fields. Print all values on submit.

```dart
// file: exercise_2_starter.dart
class _ContactFormScreenState extends State<ContactFormScreen> {
  final _formKey = GlobalKey<FormState>();
  // TODO: State variables for each field
  // TODO: TextEditingController for message (character count)
  // TODO: Build form, implement reset and submit
}
```

**Verification:** Dropdown shows four categories. Radio buttons deselect correctly. Character counter updates live. Reset clears everything including radio and checkbox state.

### Exercise 3 (Intermediate): Multi-Step Registration Wizard

Build a `RegistrationWizard` with three steps: (1) Account: email, password, confirm password; (2) Profile: first name, last name, birth date (DatePicker), gender (dropdown); (3) Preferences: notification toggle, newsletter checkbox, language dropdown, bio. Each step has its own `GlobalKey<FormState>`. "Next" validates before advancing; "Back" preserves data. Confirm password must match password. Birth date must make user at least 13. Final submit shows a summary dialog.

```dart
// file: exercise_3_starter.dart
class _RegistrationWizardState extends State<RegistrationWizard> {
  int _currentStep = 0;
  // TODO: Form keys per step, state variables for all fields
  // TODO: Step indicator, per-step validation, summary dialog
}
```

**Verification:** Mismatched passwords show error. "Next" with empty fields blocks advancement. "Back" preserves entered data. Under-13 birth date rejected. Submit shows complete summary.

### Exercise 4 (Intermediate): Async Username Validator with Debounce

Create a `UsernameRegistration` form with username, display name, and email. Username validates asynchronously with 600ms debounce. Show spinner while checking, green checkmark when available, red X when taken. Mock taken names: "admin", "user", "test", "flutter", "dart". Synchronous validation first (3-20 chars, alphanumeric + underscores), then async. Block form submission during async check. Use a custom `TextInputFormatter` to block invalid characters.

```dart
// file: exercise_4_starter.dart
class _UsernameRegistrationState extends State<UsernameRegistration> {
  // TODO: Controllers, debounce timer, async state tracking
  // TODO: InputFormatter for [a-zA-Z0-9_] only
  // TODO: Build form with live username feedback
}
```

**Verification:** "ad" shows sync error (too short), no async call. "admin" triggers spinner after 600ms, then shows taken. "myname" shows green check. Special characters blocked. Rapid typing triggers only one server call for final value.

### Exercise 5 (Advanced): Reusable Form Field Library

Create four reusable widgets: (1) `AppTextField` wrapping TextFormField with consistent decoration and optional "required" red asterisk; (2) `AppDropdown<T>` generic widget with search/filter for long lists (shows a dialog with TextField + filtered ListView); (3) `AppDateField` with read-only text field that opens DatePicker on tap; (4) `AppPhoneField` with country code dropdown and auto-formatting. All accept `validator`, `onSaved`, `enabled`, `label`. All handle focus correctly. Demo all four in an "Edit Profile" form.

```dart
// file: exercise_5_starter.dart
// TODO: AppTextField with required indicator
// TODO: AppDropdown<T> with search dialog for 10+ items
// TODO: AppDateField with DatePicker integration
// TODO: AppPhoneField with country code + formatting
// TODO: EditProfileScreen using all four widgets
```

**Verification:** Required fields show red asterisk. Long dropdown opens search dialog. Date field opens picker and formats selection. Phone auto-formats as you type. Tab moves focus logically.

### Exercise 6 (Advanced): Searchable Dropdown with Cross-Field Validation

Build a `ShippingAddressForm` with street, city, country (searchable dropdown with highlighted matches), state/province (dependent on country, mock data for 3+ countries), and postal code (validation pattern changes per country: US 5-digit/ZIP+4, UK alphanumeric, Canada A1A 1A1). Cross-field: if country has states and none selected, show error on submit. Include "Same as billing address" checkbox that auto-fills from mock data. Debounce country search.

```dart
// file: exercise_6_starter.dart
class _ShippingAddressFormState extends State<ShippingAddressForm> {
  // TODO: Mock data for countries, states, postal patterns
  // TODO: Searchable country dialog with text highlighting
  // TODO: Dependent state dropdown, country-aware postal validation
  // TODO: "Same as billing" auto-fill
}
```

**Verification:** Typing "uni" highlights and shows "United States" and "United Kingdom". Changing country clears selected state and repopulates options. Wrong postal format shows country-specific error. "Same as billing" fills all fields instantly. Submit with missing state shows error.

### Exercise 7 (Insane): Schema-Driven Dynamic Form Generator

Build a `DynamicFormEngine` that takes a JSON schema and renders a fully functional form. Support field types: text, number, email, phone, date, select, multiselect, checkbox, file, textarea, and array (repeating groups). Support conditional visibility (field depends on another field's value), multi-step wizard layout defined in schema, cross-field validation from schema rules, file upload with mime/size constraints (mock), async validation for marked fields, and field arrays (add/remove repeated groups). Return structured data matching schema hierarchy on submit. Handle malformed schemas gracefully.

```dart
// file: exercise_7_starter.dart
const sampleSchema = '''
{
  "title": "Job Application",
  "steps": [
    {"title": "Personal Info", "fields": ["name", "email", "resume"]},
    {"title": "Experience", "fields": ["years_experience", "skills", "references"]}
  ],
  "fields": {
    "name": {"type": "text", "label": "Full Name", "required": true},
    "email": {"type": "email", "label": "Email", "asyncValidation": true},
    "resume": {"type": "file", "accept": ["application/pdf"], "maxSizeMB": 5},
    "years_experience": {"type": "select", "options": ["0-1","2-4","5-9","10+"]},
    "skills": {"type": "multiselect", "options": ["Dart","Flutter","Swift"], "minSelections": 1},
    "references": {
      "type": "array", "minItems": 1, "maxItems": 5,
      "itemFields": {
        "ref_name": {"type": "text", "label": "Name", "required": true},
        "ref_email": {"type": "email", "label": "Email", "required": true}
      }
    }
  }
}
''';

class DynamicFormEngine extends StatefulWidget {
  final String schemaJson;
  final void Function(Map<String, dynamic> data)? onSubmit;
  // TODO: Parse schema into typed model, build widget tree per field type,
  // manage multi-step navigation, conditional visibility, array add/remove,
  // async validation with debounce, structured data collection on submit
}
```

**Verification:** Schema renders two-step wizard. Add/remove references works with min/max enforcement. File upload rejects wrong mime type. Async validation fires after debounce. Malformed schema shows error message, not a crash. Submit produces structured Map matching schema.

### Exercise 8 (Insane): Rich Text Editor with Formatting Toolbar

Build a `RichTextEditor` with a formatting toolbar (bold, italic, underline, strikethrough, H1-H3). Implement undo/redo with a command history stack (50 levels). Support keyboard shortcuts: Ctrl+B/I/U, Ctrl+Z/Shift+Z. Track current selection and reflect active styles in the toolbar. Toggle styles on selections. Build a document model separating styling from text. Add preview mode (read-only styled output). Export to Markdown.

```dart
// file: exercise_8_starter.dart
class RichTextEditor extends StatefulWidget {
  final String? initialMarkdown;
  final ValueChanged<String>? onChanged;
  // TODO: Document model (spans with style sets)
  // TODO: Undo/redo stacks with 50-level cap
  // TODO: Toolbar reflecting current selection styles
  // TODO: Keyboard shortcuts via Focus + onKeyEvent
  // TODO: Preview mode toggle, Markdown export
}
```

**Verification:** Select word, press Bold button -- word renders bold. Ctrl+B toggles bold. Three undos revert three steps. Ctrl+Shift+Z redoes. Cursor in bold word highlights Bold button. Preview shows styled text. Export wraps bold in `**`, italic in `*`.

---

## Summary

Forms are where your application and your users meet. In this section you learned:

- **TextField/TextEditingController** are the foundation: they manage text, cursor position, and lifecycle. Always dispose controllers.
- **Form/TextFormField** coordinate validation: `validate()` checks all fields, `save()` collects values, `reset()` clears them.
- **Input decoration theming** keeps forms visually consistent without repetition.
- **FocusNode** controls keyboard navigation order and auto-advance.
- **Input formatters** constrain input at the source, before validation runs.
- **GestureDetector** and drag-and-drop extend forms beyond text into touch interfaces.
- **Selection widgets** follow a consistent value-plus-callback pattern.
- **Debouncing/async validation** check server constraints without degrading typing.
- **Accessibility** is a requirement: semantic labels, logical focus order, 48px touch targets.

## What's Next

In **Section 14: Networking & Data**, you will connect forms to real backends. You will send the validated data to REST APIs, handle loading and error states during submission, parse server responses, and manage optimistic updates. The validation patterns you learned here will combine with network error handling to create robust data flows from the user's fingertips to your server and back.

## References

- [Flutter Forms Cookbook](https://docs.flutter.dev/cookbook/forms) -- Official form recipes
- [Form class API](https://api.flutter.dev/flutter/widgets/Form-class.html) -- Form widget documentation
- [TextFormField API](https://api.flutter.dev/flutter/material/TextFormField-class.html) -- Field-level validation
- [GestureDetector API](https://api.flutter.dev/flutter/widgets/GestureDetector-class.html) -- Gesture handling
- [Draggable API](https://api.flutter.dev/flutter/widgets/Draggable-class.html) -- Drag and drop system
- [Flutter Input Decoration](https://api.flutter.dev/flutter/material/InputDecoration-class.html) -- Theming inputs
- [Focus System](https://docs.flutter.dev/development/ui/advanced/focus) -- Focus management guide
- [Accessibility in Flutter](https://docs.flutter.dev/development/accessibility-and-localization/accessibility) -- Semantic labels and screen readers
