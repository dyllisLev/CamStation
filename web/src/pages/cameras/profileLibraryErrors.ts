export class ProfileLibraryValidationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ProfileLibraryValidationError";
  }
}
