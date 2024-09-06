# sns

The `sns` package provides tooling to verify the authenticity of an Amazon SNS (Simple Notification Service) Payload. The validation process adheres to the guidelines outlined in the [AWS Documentation](https://docs.aws.amazon.com/sns/latest/dg/sns-verify-signature-of-message.html).

### Key Features

- Ensures the `SigningCertURL` originates from a valid Amazon SNS domain.
- Supports both the standard AWS regions (`https://sns.<your-region>.amazonaws.com`) and the AWS China regions (`https://sns.<your-region>.amazonaws.com.cn`).

### Important Notes

**1. TopicArn Validation:**  
This library does **NOT** perform validation on the `TopicArn`. Users of this library are responsible for handling this validation on their own.

**2. SigningCertURL Whitelisting:**  
For added security, it is highly recommended to whitelist the `SigningCertURL` host against the specific AWS regions you're actively using (e.g., `https://sns.us-west-1.amazonaws.com`). This ensures that the certificate URL matches the intended region.
