import { PartialType } from '@nestjs/swagger';
import { CreateBlogPostDto } from './create-blog-post.dto';

export class UpdateBlogPostDto extends PartialType(CreateBlogPostDto) {
  override readonly title!: string;
  override readonly subtitle!: string;
  override readonly description!: string;
  override readonly thumbnail!: string;
  override readonly downloadLink!: string;
  // override readonly dateTime!: Date;
}
