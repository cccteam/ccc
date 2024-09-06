# sns

The sns package provides tooling to verify the authenticity of a SNS Payload. The validation process follows the general guidance provided in [AWS Documentation](ttps://docs.aws.amazon.com/sns/latest/dg/sns-verify-signature-of-message.html). Verifying the certificate was received from Amazon SNS is done by ensuring the SigningCertURL points to a https://sns.your-zone-1.amazonaws.com url or a https://sns.your-zone-1.amazonaws.com.cn url.

### IMPORTANT

This library does NOT validate the TopicArn. This is left to the consumer of the library. As added validation, it is encurage that the SigningCertURL Host is whitelisted against a list of zones that you are actually using. (i.e. https://sns.us-west-1.amazonaws.com)
