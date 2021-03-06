import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import { Document } from 'mongoose';

// import { BlogPost } from '@sick/api/blog-post';

export type UserDocument = User & Document;

@Schema()
export class User {
  @Prop({ required: true })
  username!: string;

  @Prop({ required: true })
  email!: string;

  @Prop()
  avatar!: string;

  // @Prop({ type: mongoose.Schema.Types.ObjectId, ref: 'BlogPost' })
  // blogPosts: BlogPost[];
}

export const UserSchema = SchemaFactory.createForClass(User);
