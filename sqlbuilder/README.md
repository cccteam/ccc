# sqlbuilder

Principles are:

- statements are concerned only with formatting themselves and they should not worry how they are formatted in relation to their parent
  - for example: an `Identifier` should only return its `name` when its `Sql()` method is called and not `" " + name`
- delay error setting till the last possible moment, the intention is to allow the objects to go through invalid intermediate states if necessary
- composite statements should take variadics in their constructors, we may know only some of the statements definition at initialization time
- when calling the `Sql()` methods, they should return both an error if exists as well as the string representation of the current state
